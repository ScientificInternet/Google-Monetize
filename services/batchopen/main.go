package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"database/sql"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/panjf2000/ants/v2"
	"github.com/prometheus/client_golang/prometheus"
	pcache "github.com/ScientificInternet/Google-Monetize/pkg/cache"
	"github.com/ScientificInternet/Google-Monetize/pkg/errors"
	ev "github.com/ScientificInternet/Google-Monetize/pkg/events"
	httpx "github.com/ScientificInternet/Google-Monetize/pkg/http"
	"github.com/ScientificInternet/Google-Monetize/pkg/middleware"
	"github.com/ScientificInternet/Google-Monetize/pkg/serviceclient"
	"github.com/ScientificInternet/Google-Monetize/pkg/telemetry"
	api "github.com/ScientificInternet/Google-Monetize/services/batchopen/internal/oapi"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Global service registry for calling other microservices
var serviceRegistry *serviceclient.Registry

type createTaskRequest struct {
	OfferID          string                 `json:"offerId"`
	SimulationConfig map[string]interface{} `json:"simulationConfig"`
}

type createTaskResponse struct {
	TaskID    string    `json:"taskId"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type taskItem struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	OfferID        string `json:"offer_id,omitempty"`
	OfferURL       string `json:"offer_url,omitempty"`
	TokensConsumed int    `json:"tokens_consumed"`
	Progress       *int   `json:"progress,omitempty"`
	CurrentStep    string `json:"current_step,omitempty"`
	Error          string `json:"error,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

type tasksListResponse struct {
	Tasks      []taskItem `json:"tasks"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	TotalPages int        `json:"total_pages"`
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
func ready(w http.ResponseWriter, r *http.Request) {
	// Check DB if configured
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
		defer cancel()
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "db open failed", map[string]string{"error": err.Error()})
			return
		}
		defer db.Close()
		if err := db.PingContext(ctx); err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "db not ready", map[string]string{"error": err.Error()})
			return
		}
	}
	// Check Redis if configured
	if strings.TrimSpace(os.Getenv("REDIS_URL")) != "" {
		rc := pcache.NewFromEnv()
		if rc == nil || !rc.Ready() {
			errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "redis client not ready", nil)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
		defer cancel()
		if err := rc.Redis().Ping(ctx).Err(); err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "NOT_READY", "redis ping failed", map[string]string{"redis": err.Error()})
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
func stats(w http.ResponseWriter, r *http.Request) {
	hit := atomic.LoadInt64(&cacheHits)
	miss := atomic.LoadInt64(&cacheMiss)
	total := hit + miss
	var hitRate int
	if total > 0 {
		hitRate = int((hit * 100) / total)
	}
	// request/error counters are Prometheus-only here; keep JSON minimal
	writeJSON(w, http.StatusOK, map[string]any{
		"inflight":     atomic.LoadInt32(&inflightCur),
		"cacheHits":    hit,
		"cacheMiss":    miss,
		"cacheHitRate": hitRate, // percent
	})
}

// goneHandler is used for retired endpoints
func goneHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusGone, map[string]any{
		"error": map[string]any{
			"code":    "FEATURE_RETIRED",
			"message": "Autoclick feature has been retired",
		},
	})
}

func createTaskHandler(pub *ev.Publisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			return
		}
		uid, _ := r.Context().Value(middleware.UserIDKey).(string)
		// Idempotency: if key provided and existing mapping found, short-circuit
		if idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key")); idem != "" {
			if ex, ok := lookupIdem(r.Context(), idem, uid, "batchopen.create"); ok && ex != "" {
				writeJSON(w, http.StatusAccepted, map[string]any{"taskId": ex, "status": "queued", "idempotent": true})
				return
			}
		}
		var req createTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid request body", nil)
			return
		}
		if req.OfferID == "" {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "offerId is required", nil)
			return
		}
		resp := createTaskResponse{
			TaskID:    uuid.New().String(),
			Status:    "queued",
			CreatedAt: time.Now(),
		}
		// Persist idempotency mapping best-effort
		if idem := strings.TrimSpace(r.Header.Get("X-Idempotency-Key")); idem != "" {
			_ = upsertIdem(r.Context(), idem, uid, "batchopen.create", resp.TaskID, 24*time.Hour)
		}
		// Publish event (best-effort)
		if pub != nil {
			_ = pub.Publish(r.Context(), ev.EventBatchOpsTaskQueued, map[string]any{
				"taskId":   resp.TaskID,
				"offerId":  req.OfferID,
				"userId":   uid,
				"queuedAt": resp.CreatedAt.UTC().Format(time.RFC3339),
			}, ev.WithSource("batchopen"), ev.WithSubject(resp.TaskID))
		}
		// Firestore UI cache (best-effort)
		_ = writeTaskUI(r.Context(), uid, resp.TaskID, req.OfferID, resp.Status, resp.CreatedAt)
		writeJSON(w, http.StatusAccepted, resp)

		// Background execution via Browser-Exec (best-effort)
		submitBackgroundTask(func() {
			ctx := context.Background()
			// 1) reserve tokens
			_ = billingAction(ctx, uid, "reserve", resp.TaskID)
			// 2) fetch offer url
			url := fetchOfferURL(ctx, req.OfferID, uid)
			if url == "" {
				_ = updateTaskUI(ctx, uid, resp.TaskID, map[string]any{"status": "failed", "error": "offer_url_not_found"})
				if pub != nil {
					_ = pub.Publish(ctx, ev.EventBatchOpsTaskFailed, map[string]any{"taskId": resp.TaskID, "userId": uid, "failedAt": time.Now().UTC().Format(time.RFC3339), "reason": "offer_url_not_found"}, ev.WithSource("batchopen"), ev.WithSubject(resp.TaskID))
				}
				_ = billingAction(ctx, uid, "release", resp.TaskID)
				return
			}
			// 3) call browser-exec
			// announce browser exec request
			if pub != nil {
				_ = pub.Publish(ctx, ev.EventBrowserExecRequested, map[string]any{"taskId": resp.TaskID, "userId": uid, "url": url, "requestedAt": time.Now().UTC().Format(time.RFC3339)}, ev.WithSource("batchopen"), ev.WithSubject(resp.TaskID))
			}
			ok, beRes := browserExecCheckWithRetry(ctx, url)
			if ok {
				// compute simple quality score
				qScore, qFactors := computeQuality(beRes)
				_ = updateTaskUI(ctx, uid, resp.TaskID, map[string]any{"status": "completed", "result": beRes, "quality": map[string]any{"score": qScore, "factors": qFactors}})
				if pub != nil {
					_ = pub.Publish(ctx, ev.EventBrowserExecCompleted, map[string]any{"taskId": resp.TaskID, "userId": uid, "completedAt": time.Now().UTC().Format(time.RFC3339), "ok": true, "quality": qScore}, ev.WithSource("batchopen"), ev.WithSubject(resp.TaskID))
					_ = pub.Publish(ctx, ev.EventBatchOpsTaskCompleted, map[string]any{"taskId": resp.TaskID, "userId": uid, "completedAt": time.Now().UTC().Format(time.RFC3339), "result": beRes, "quality": qScore}, ev.WithSource("batchopen"), ev.WithSubject(resp.TaskID))
				}
				_ = billingAction(ctx, uid, "commit", resp.TaskID)
			} else {
				qScore, qFactors := computeQuality(beRes)
				_ = updateTaskUI(ctx, uid, resp.TaskID, map[string]any{"status": "failed", "result": beRes, "quality": map[string]any{"score": qScore, "factors": qFactors}})
				if pub != nil {
					_ = pub.Publish(ctx, ev.EventBrowserExecCompleted, map[string]any{"taskId": resp.TaskID, "userId": uid, "completedAt": time.Now().UTC().Format(time.RFC3339), "ok": false, "quality": qScore}, ev.WithSource("batchopen"), ev.WithSubject(resp.TaskID))
					_ = pub.Publish(ctx, ev.EventBatchOpsTaskFailed, map[string]any{"taskId": resp.TaskID, "userId": uid, "failedAt": time.Now().UTC().Format(time.RFC3339), "reason": beRes["error"]}, ev.WithSource("batchopen"), ev.WithSubject(resp.TaskID))
				}
				_ = billingAction(ctx, uid, "release", resp.TaskID)
			}
		})
	}
}

// --- Minimal idempotency helpers (shared table) ---
var (
	idemDB   *sql.DB
	idemOnce sync.Once
)

var (
	tasksDB    *sql.DB
	tasksOnce  sync.Once
	tasksDBErr error
)

var (
	taskPool     *ants.Pool
	taskPoolOnce sync.Once
	taskPoolErr  error
)

func ensureTaskPool() *ants.Pool {
	taskPoolOnce.Do(func() {
		size := 10
		if v := strings.TrimSpace(os.Getenv("BATCHOPEN_WORKER_POOL_SIZE")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 128 {
				size = n
			}
		}
		pool, err := ants.NewPool(size, ants.WithPanicHandler(func(p interface{}) {
			log.Printf("batchopen: background worker panic: %v", p)
		}))
		if err != nil {
			taskPoolErr = err
			log.Printf("batchopen: failed to create worker pool, fallback to goroutines: %v", err)
			return
		}
		taskPool = pool
	})
	return taskPool
}

func submitBackgroundTask(job func()) {
	if pool := ensureTaskPool(); pool != nil {
		if err := pool.Submit(job); err == nil {
			return
		} else {
			log.Printf("batchopen: pool submit failed (%v), fallback to goroutine", err)
		}
	}
	go job()
}

func idemConn() *sql.DB {
	idemOnce.Do(func() {
		dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
		if dsn == "" {
			return
		}
		db, err := sql.Open("postgres", dsn)
		if err == nil {
			// let db pooling handle reuse; don't Ping here to avoid cold start blocking
			idemDB = db
		}
	})
	return idemDB
}

func tasksConn() (*sql.DB, error) {
	tasksOnce.Do(func() {
		dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
		if dsn == "" {
			tasksDBErr = fmt.Errorf("DATABASE_URL not configured")
			return
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			tasksDBErr = err
			return
		}
		tasksDB = db
	})
	return tasksDB, tasksDBErr
}

func lookupIdem(ctx context.Context, key, userID, scope string) (string, bool) {
	db := idemConn()
	if db == nil {
		return "", false
	}
	var id string
	c, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	defer cancel()
	err := db.QueryRowContext(c, `SELECT target_id FROM idempotency_keys WHERE key=$1 AND user_id=$2 AND scope=$3 AND expires_at>NOW()`, key, userID, scope).Scan(&id)
	if err != nil {
		return "", false
	}
	return id, id != ""
}

func upsertIdem(ctx context.Context, key, userID, scope, targetID string, ttl time.Duration) error {
	db := idemConn()
	if db == nil {
		return nil
	}
	c, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	_, err := db.ExecContext(c, `
        INSERT INTO idempotency_keys(key, user_id, scope, target_id, created_at, expires_at)
        VALUES ($1,$2,$3,$4,NOW(), NOW()+$5::interval)
        ON CONFLICT (key) DO UPDATE SET user_id=EXCLUDED.user_id, scope=EXCLUDED.scope, target_id=EXCLUDED.target_id, expires_at=EXCLUDED.expires_at
    `, key, userID, scope, targetID, fmt.Sprintf("%d hours", int(ttl.Hours())))
	return err
}

// taskActionHandler supports /api/v1/batchopen/tasks/{id}/start|complete|fail
func taskActionHandler(pub *ev.Publisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			return
		}
		uid, _ := r.Context().Value(middleware.UserIDKey).(string)
		// path parsing
		p := strings.TrimPrefix(r.URL.Path, "/api/v1/batchopen/tasks/")
		seg := strings.Split(p, "/")
		if len(seg) < 2 {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "taskId and action required", nil)
			return
		}
		taskID, action := strings.TrimSpace(seg[0]), strings.TrimSpace(seg[1])
		if taskID == "" {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "taskId required", nil)
			return
		}
		now := time.Now().UTC()
		switch action {
		case "start":
			if pub != nil {
				_ = pub.Publish(r.Context(), ev.EventBatchOpsTaskStarted, map[string]any{"taskId": taskID, "userId": uid, "startedAt": now.Format(time.RFC3339)}, ev.WithSource("batchopen"), ev.WithSubject(taskID))
			}
			if pub != nil {
				_ = pub.Publish(r.Context(), ev.EventWorkflowStarted, map[string]any{"workflow": "batchopen", "taskId": taskID, "userId": uid, "startedAt": now.Format(time.RFC3339)}, ev.WithSource("batchopen"), ev.WithSubject(taskID))
			}
			_ = writeTaskUI(r.Context(), uid, taskID, "", "running", now)
			_ = billingAction(r.Context(), uid, "reserve", taskID)
			writeJSON(w, http.StatusOK, map[string]any{"taskId": taskID, "status": "running"})
		case "complete":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if pub != nil {
				_ = pub.Publish(r.Context(), ev.EventBatchOpsTaskCompleted, map[string]any{"taskId": taskID, "userId": uid, "completedAt": now.Format(time.RFC3339), "result": body}, ev.WithSource("batchopen"), ev.WithSubject(taskID))
			}
			if pub != nil {
				_ = pub.Publish(r.Context(), ev.EventWorkflowCompleted, map[string]any{"workflow": "batchopen", "taskId": taskID, "userId": uid, "completedAt": now.Format(time.RFC3339)}, ev.WithSource("batchopen"), ev.WithSubject(taskID))
			}
			_ = writeTaskUI(r.Context(), uid, taskID, "", "completed", now)
			_ = billingAction(r.Context(), uid, "commit", taskID)
			writeJSON(w, http.StatusOK, map[string]any{"taskId": taskID, "status": "completed"})
		case "fail":
			var body struct {
				Reason string `json:"reason"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if pub != nil {
				_ = pub.Publish(r.Context(), ev.EventBatchOpsTaskFailed, map[string]any{"taskId": taskID, "userId": uid, "failedAt": now.Format(time.RFC3339), "reason": body.Reason}, ev.WithSource("batchopen"), ev.WithSubject(taskID))
			}
			if pub != nil {
				_ = pub.Publish(r.Context(), ev.EventWorkflowStepCompleted, map[string]any{"workflow": "batchopen", "taskId": taskID, "userId": uid, "step": "fail", "time": now.Format(time.RFC3339), "status": "failed"}, ev.WithSource("batchopen"), ev.WithSubject(taskID))
			}
			_ = writeTaskUI(r.Context(), uid, taskID, "", "failed", now)
			_ = billingAction(r.Context(), uid, "release", taskID)
			writeJSON(w, http.StatusOK, map[string]any{"taskId": taskID, "status": "failed"})
		default:
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "unknown action", map[string]string{"action": action})
		}
	}
}

func listUserTasks(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context", nil)
		return
	}

	opts := parseTasksQueryOptions(r)

	resp, err := queryUserTasks(r.Context(), uid, opts)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "failed to load tasks", map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

type tasksQueryOptions struct {
	Limit      int
	Page       int
	Statuses   []string
	SortColumn string
	SortOrder  string
}

func parseTasksQueryOptions(r *http.Request) tasksQueryOptions {
	opt := tasksQueryOptions{
		Limit:      20,
		Page:       1,
		SortColumn: resolveTaskSortColumn(strings.TrimSpace(r.URL.Query().Get("sort_by"))),
		SortOrder:  resolveTaskSortOrder(strings.TrimSpace(r.URL.Query().Get("sort_order"))),
		Statuses:   normalizeTaskStatusFilter(r.URL.Query().Get("status")),
	}

	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			switch {
			case n < 1:
				opt.Limit = 1
			case n > 100:
				opt.Limit = 100
			default:
				opt.Limit = n
			}
		}
	}

	if v := strings.TrimSpace(r.URL.Query().Get("page")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opt.Page = n
		}
	}

	return opt
}

func queryUserTasks(ctx context.Context, uid string, opts tasksQueryOptions) (tasksListResponse, error) {
	db, err := tasksConn()
	if err != nil || db == nil {
		return tasksListResponse{
			Tasks:      []taskItem{},
			Total:      0,
			Page:       opts.Page,
			Limit:      opts.Limit,
			TotalPages: 0,
		}, nil
	}

	countQuery := `SELECT COUNT(*) FROM "BatchopenTask" t WHERE t."userId" = $1`
	countArgs := []any{uid}
	if len(opts.Statuses) > 0 {
		placeholders := make([]string, len(opts.Statuses))
		for i := range opts.Statuses {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
		}
		countQuery += " AND t.status IN (" + strings.Join(placeholders, ",") + ")"
		for _, s := range opts.Statuses {
			countArgs = append(countArgs, s)
		}
	}

	var total int
	if err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return tasksListResponse{}, err
	}

	offset := (opts.Page - 1) * opts.Limit
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT t.id,
		       COALESCE(t."offerId", ''),
		       t.status,
		       t.created_at,
		       t.updated_at,
		       COALESCE(t.result::text, 'null'),
		       COALESCE(o."originalUrl", '')
		FROM "BatchopenTask" t
		LEFT JOIN "Offer" o ON o.id = t."offerId"
		WHERE t."userId" = $1`

	args := []any{uid}
	nextIdx := 2
	if len(opts.Statuses) > 0 {
		placeholders := make([]string, len(opts.Statuses))
		for i, s := range opts.Statuses {
			placeholders[i] = fmt.Sprintf("$%d", nextIdx+i)
			args = append(args, s)
		}
		query += " AND t.status IN (" + strings.Join(placeholders, ",") + ")"
		nextIdx += len(opts.Statuses)
	}

	query += fmt.Sprintf(" ORDER BY t.%s %s", opts.SortColumn, opts.SortOrder)
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", nextIdx, nextIdx+1)
	args = append(args, opts.Limit, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return tasksListResponse{}, err
	}
	defer rows.Close()

	var tasks []taskItem
	for rows.Next() {
		var (
			id        string
			offerID   sql.NullString
			status    string
			createdAt sql.NullTime
			updatedAt sql.NullTime
			rawResult sql.NullString
			offerURL  sql.NullString
		)

		if err := rows.Scan(&id, &offerID, &status, &createdAt, &updatedAt, &rawResult, &offerURL); err != nil {
			continue
		}

		tasks = append(tasks, buildTaskItem(id, offerID, status, createdAt, updatedAt, rawResult, offerURL))
	}

	totalPages := 0
	if opts.Limit > 0 {
		totalPages = (total + opts.Limit - 1) / opts.Limit
	}

	return tasksListResponse{
		Tasks:      tasks,
		Total:      total,
		Page:       opts.Page,
		Limit:      opts.Limit,
		TotalPages: totalPages,
	}, nil
}

func streamUserTasks(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "streaming unsupported", nil)
		return
	}

	opts := parseTasksQueryOptions(r)
	ctx := r.Context()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sendSnapshot := func() (bool, error) {
		resp, err := queryUserTasks(ctx, uid, opts)
		if err != nil {
			return false, err
		}

		payload, err := json.Marshal(resp)
		if err != nil {
			return false, err
		}

		if _, err := fmt.Fprintf(w, "event: tasks\ndata: %s\n\n", payload); err != nil {
			return false, err
		}

		flusher.Flush()
		return hasActiveTasks(resp.Tasks), nil
	}

	active, err := sendSnapshot()
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[tasks] stream initial snapshot failed: %v", err)
		}
		return
	}

	interval := 20 * time.Second
	if active {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active, err := sendSnapshot()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[tasks] stream send failed: %v", err)
				}
				return
			}

			desired := 20 * time.Second
			if active {
				desired = 5 * time.Second
			}
			if desired != interval {
				interval = desired
				ticker.Reset(interval)
			}
		}
	}
}

func hasActiveTasks(tasks []taskItem) bool {
	for _, task := range tasks {
		switch task.Status {
		case "pending", "running":
			return true
		}
	}
	return false
}

func getUserTask(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context", nil)
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "task id required", nil)
		return
	}

	db, err := tasksConn()
	if err != nil || db == nil {
		errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "task not found", nil)
		return
	}

	query := `
        SELECT t.id,
               COALESCE(t."offerId", ''),
               t.status,
               t.created_at,
               t.updated_at,
               COALESCE(t.result::text, 'null'),
               COALESCE(o."originalUrl", '')
        FROM "BatchopenTask" t
        LEFT JOIN "Offer" o ON o.id = t."offerId"
        WHERE t."userId" = $1 AND t.id = $2
        LIMIT 1
    `

	var (
		offerID   sql.NullString
		status    string
		createdAt sql.NullTime
		updatedAt sql.NullTime
		rawResult sql.NullString
		offerURL  sql.NullString
	)

	err = db.QueryRowContext(r.Context(), query, uid, id).Scan(&id, &offerID, &status, &createdAt, &updatedAt, &rawResult, &offerURL)
	if err == sql.ErrNoRows {
		errors.Write(w, r, http.StatusNotFound, "NOT_FOUND", "task not found", nil)
		return
	}
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query task failed", map[string]string{"error": err.Error()})
		return
	}

	item := buildTaskItem(id, offerID, status, createdAt, updatedAt, rawResult, offerURL)
	writeJSON(w, http.StatusOK, item)
}

func cancelUserTask(pub *ev.Publisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
			return
		}

		uid, _ := r.Context().Value(middleware.UserIDKey).(string)
		if uid == "" {
			errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context", nil)
			return
		}

		id := strings.TrimSpace(chi.URLParam(r, "id"))
		if id == "" {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "task id required", nil)
			return
		}

		db, err := tasksConn()
		if err != nil || db == nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "database not available", nil)
			return
		}

		res, err := db.ExecContext(r.Context(), `
            UPDATE "BatchopenTask"
            SET status = 'cancelled', updated_at = NOW()
            WHERE id = $1 AND "userId" = $2 AND status IN ('queued', 'running')
        `, id, uid)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "cancel task failed", map[string]string{"error": err.Error()})
			return
		}

		if affected, _ := res.RowsAffected(); affected == 0 {
			errors.Write(w, r, http.StatusConflict, "INVALID_STATE", "task is not cancellable", nil)
			return
		}

		_ = updateTaskUI(r.Context(), uid, id, map[string]any{"status": "cancelled", "cancelledAt": time.Now().UTC().Format(time.RFC3339)})
		_ = billingAction(r.Context(), uid, "release", id)

		writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "status": "cancelled"})
	}
}

func buildTaskItem(id string, offerID sql.NullString, status string, createdAt sql.NullTime, updatedAt sql.NullTime, rawResult sql.NullString, offerURL sql.NullString) taskItem {
	item := taskItem{
		ID:             id,
		Type:           "evaluation",
		Status:         mapTaskStatus(status),
		TokensConsumed: 0,
	}

	if offerID.Valid {
		item.OfferID = offerID.String
	}
	if offerURL.Valid {
		item.OfferURL = offerURL.String
	}

	if createdAt.Valid {
		item.CreatedAt = createdAt.Time.UTC().Format(time.RFC3339)
	}
	if updatedAt.Valid {
		item.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
	}

	if rawResult.Valid && rawResult.String != "null" && rawResult.String != "" {
		tokens, step, progress, errMsg := parseTaskResult(rawResult.String)
		if tokens > 0 {
			item.TokensConsumed = tokens
		}
		if step != "" {
			item.CurrentStep = step
		}
		if progress != nil {
			item.Progress = progress
		}
		if errMsg != "" {
			item.Error = errMsg
		}
	}

	return item
}

func parseTaskResult(raw string) (int, string, *int, string) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, "", nil, ""
	}

	tokens := extractInt(payload, "tokens", "tokensConsumed", "token_cost", "tokenCost")
	if tokens < 0 {
		tokens = -tokens
	}

	step := extractString(payload, "current_step", "currentStep", "step")

	var progressPtr *int
	if v, ok := payload["progress"].(float64); ok {
		p := int(v)
		if p < 0 {
			p = 0
		} else if p > 100 {
			p = 100
		}
		progressPtr = &p
	}

	errMsg := extractString(payload, "error", "message")

	return tokens, step, progressPtr, errMsg
}

func extractInt(payload map[string]any, keys ...string) int {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			switch val := v.(type) {
			case float64:
				return int(val)
			case int:
				return val
			case int64:
				return int(val)
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					return n
				}
			}
		}
	}
	return 0
}

func extractString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func mapTaskStatus(status string) string {
	switch strings.ToLower(status) {
	case "queued", "pending":
		return "pending"
	case "running", "processing":
		return "running"
	case "completed", "done":
		return "completed"
	case "failed", "error":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return "pending"
	}
}

func normalizeTaskStatusFilter(raw string) []string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all":
		return nil
	case "pending":
		return []string{"queued"}
	case "running":
		return []string{"running"}
	case "completed":
		return []string{"completed"}
	case "failed":
		return []string{"failed"}
	case "cancelled", "canceled":
		return []string{"cancelled"}
	default:
		return nil
	}
}

func resolveTaskSortColumn(raw string) string {
	switch strings.ToLower(raw) {
	case "updated_at":
		return "updated_at"
	default:
		return "created_at"
	}
}

func resolveTaskSortOrder(raw string) string {
	switch strings.ToLower(raw) {
	case "asc", "ascending":
		return "ASC"
	default:
		return "DESC"
	}
}

func main() {
	log.Println("Starting Batchopen service...")

	// Initialize service registry for microservice calls
	serviceRegistry = serviceclient.NewRegistry()
	log.Println("Service registry initialized")

	// Setup distributed tracing (no-op if TRACES_ENABLED != 1)
	shutdown := telemetry.SetupTracing("batchopen")
	defer func() { _ = shutdown(context.Background()) }()

	ctx := context.Background()
	// unified auth via pkg/middleware.AuthMiddleware
	var pub *ev.Publisher
	if p, err := ev.NewPublisher(ctx); err == nil {
		pub = p
		defer p.Close()
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID())
	telemetry.RegisterDefaultMetrics("batchopen")
	// Middlewares must be defined before routes
	r.Use(telemetry.ChiMiddleware("batchopen"))
	r.Use(middleware.LoggingMiddleware("batchopen"))
	r.Use(middleware.SecurityHeaders())
	r.Handle("/metrics", telemetry.MetricsHandler())
	r.Get("/health", health)
	r.Get("/readyz", ready)
	r.Get("/api/v1/batchopen/stats", stats)
	// Autoclick endpoints retired
	r.Handle("/api/v1/batchopen/autoclick/analysis", middleware.AuthMiddleware(http.HandlerFunc(goneHandler)))
	r.Handle("/api/v1/batchopen/autoclick/tasks", middleware.AuthMiddleware(http.HandlerFunc(goneHandler)))
	r.Handle("/api/v1/batchopen/autoclick/execute-tick", middleware.AuthMiddleware(http.HandlerFunc(goneHandler)))

	r.Group(func(rr chi.Router) {
		rr.Use(middleware.AuthMiddleware)
		rr.Get("/api/v1/tasks", listUserTasks)
		rr.Get("/api/v1/tasks/stream", streamUserTasks)
		rr.Get("/api/v1/tasks/{id}", getUserTask)
		rr.Post("/api/v1/tasks/{id}/cancel", cancelUserTask(pub))
	})
	// OpenAPI chi server mount with auth middleware
	oas := &oasImpl{pub: pub}
	oapiHandler := api.HandlerWithOptions(oas, api.ChiServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []api.MiddlewareFunc{
			func(next http.Handler) http.Handler { return middleware.IdempotencyMiddleware(next) },
			func(next http.Handler) http.Handler { return middleware.AuthMiddleware(next) },
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		},
	})
	r.Mount("/", oapiHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// oasImpl implements OpenAPI server interface and delegates to existing logic
type oasImpl struct{ pub *ev.Publisher }

func (h *oasImpl) CreateBatchopenTask(w http.ResponseWriter, r *http.Request) {
	// reuse existing logic
	createTaskHandler(h.pub)(w, r)
}
func (h *oasImpl) ListBatchopenTasks(w http.ResponseWriter, r *http.Request) {
	// Minimal SQL read from BatchopenTask when DATABASE_URL is set; fallback 200 empty
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if dsn == "" || uid == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "db open failed", nil)
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(r.Context(), `SELECT id, "userId", "offerId", status, created_at, updated_at, COALESCE(result::text,'null') FROM "BatchopenTask" WHERE "userId"=$1 ORDER BY updated_at DESC LIMIT 50`, uid)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "INTERNAL", "query failed", nil)
		return
	}
	defer rows.Close()
	type item struct {
		ID, UserID, OfferID, Status, CreatedAt, UpdatedAt string
		Result                                            json.RawMessage
	}
	var items []item
	for rows.Next() {
		var it item
		var created, updated sql.NullTime
		var resultText string
		if err := rows.Scan(&it.ID, &it.UserID, &it.OfferID, &it.Status, &created, &updated, &resultText); err != nil {
			continue
		}
		if created.Valid {
			it.CreatedAt = created.Time.UTC().Format(time.RFC3339)
		}
		if updated.Valid {
			it.UpdatedAt = updated.Time.UTC().Format(time.RFC3339)
		}
		if resultText != "" && resultText != "null" {
			it.Result = json.RawMessage(resultText)
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (h *oasImpl) StartBatchopenTask(w http.ResponseWriter, r *http.Request, id string) {
	// synthesize path and call handler
	r = r.Clone(r.Context())
	r.URL.Path = "/api/v1/batchopen/tasks/" + id + "/start"
	taskActionHandler(h.pub)(w, r)
}
func (h *oasImpl) CompleteBatchopenTask(w http.ResponseWriter, r *http.Request, id string) {
	r = r.Clone(r.Context())
	r.URL.Path = "/api/v1/batchopen/tasks/" + id + "/complete"
	taskActionHandler(h.pub)(w, r)
}
func (h *oasImpl) FailBatchopenTask(w http.ResponseWriter, r *http.Request, id string) {
	r = r.Clone(r.Context())
	r.URL.Path = "/api/v1/batchopen/tasks/" + id + "/fail"
	taskActionHandler(h.pub)(w, r)
}

// GET /batchopen/templates
func (h *oasImpl) ListSimulationTemplates(w http.ResponseWriter, r *http.Request) {
	// minimal built-in templates
	resp := map[string]any{
		"countries": []string{"US", "GB", "DE", "FR", "JP", "SG"},
		"userAgents": []string{
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0 Safari/537.36",
		},
		"referrers": []string{"https://www.google.com/", "https://www.bing.com/", "https://duckduckgo.com/"},
		"timezones": []string{"America/Los_Angeles", "Europe/London", "Europe/Berlin", "Asia/Tokyo", "Asia/Singapore"},
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeTaskUI(ctx context.Context, userID, taskID, offerID, status string, createdAt time.Time) error {
	if strings.TrimSpace(os.Getenv("FIRESTORE_ENABLED")) != "1" {
		return nil
	}
	projectID := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("PROJECT_ID"))
	}
	if projectID == "" || userID == "" || taskID == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	cli, err := firestore.NewClient(cctx, projectID)
	if err != nil {
		return err
	}
	defer cli.Close()
	doc := map[string]any{"taskId": taskID, "offerId": offerID, "status": status, "createdAt": createdAt.UTC()}
	_, err = cli.Collection("users/"+userID+"/batchopen/tasks").Doc(taskID).Set(cctx, doc)
	return err
}

func updateTaskUI(ctx context.Context, userID, taskID string, patch map[string]any) error {
	if strings.TrimSpace(os.Getenv("FIRESTORE_ENABLED")) != "1" {
		return nil
	}
	projectID := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("PROJECT_ID"))
	}
	if projectID == "" || userID == "" || taskID == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	cli, err := firestore.NewClient(cctx, projectID)
	if err != nil {
		return err
	}
	defer cli.Close()
	patch["updatedAt"] = time.Now().UTC()
	_, err = cli.Collection("users/"+userID+"/batchopen/tasks").Doc(taskID).Set(cctx, patch)
	return err
}

func fetchOfferURL(ctx context.Context, offerID, userID string) string {
	if offerID == "" || userID == "" || serviceRegistry == nil {
		return ""
	}

	var out struct {
		OriginalUrl string `json:"originalUrl"`
	}

	err := serviceRegistry.CallJSON(ctx, "offer", serviceclient.Request{
		Method:  http.MethodGet,
		Path:    "/api/v1/offers/" + offerID,
		Headers: map[string]string{"X-User-Id": userID},
	}, &out)

	if err != nil {
		return ""
	}
	return strings.TrimSpace(out.OriginalUrl)
}

// --- Concurrency & host-level cache ---
var (
	inflightOnce sync.Once
	inflightSem  chan struct{}
	hostCache    = map[string]hostCacheEntry{}
	hostCacheMu  sync.RWMutex
	sfMu         sync.Mutex
	sfWaiters    = map[string][]chan singleflightRes{}
	inflightCur  int32
	cacheHits    int64
	cacheMiss    int64
)

type hostCacheEntry struct {
	ok  bool
	res map[string]any
	exp time.Time
}
type singleflightRes struct {
	ok  bool
	res map[string]any
}

// metrics
var (
	metricInflight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "batchopen_inflight_current",
		Help: "Current in-flight browser-exec checks",
	})
	metricCacheHits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "batchopen_host_cache_hits_total",
		Help: "Total host-level cache hits",
	})
	metricCacheMiss = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "batchopen_host_cache_miss_total",
		Help: "Total host-level cache misses",
	})
	metricRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "batchopen_requests_total",
		Help: "Total availability requests via Browser-Exec",
	})
	metricErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "batchopen_errors_total",
		Help: "Total failed availability checks",
	})
)

func init() {
	_ = prometheus.Register(metricInflight)
	_ = prometheus.Register(metricCacheHits)
	_ = prometheus.Register(metricCacheMiss)
	_ = prometheus.Register(metricRequests)
	_ = prometheus.Register(metricErrors)
}

func initInflight() {
	inflightOnce.Do(func() {
		max := 8
		if v := strings.TrimSpace(os.Getenv("BATCHOPEN_MAX_INFLIGHT")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 64 {
				max = n
			}
		}
		inflightSem = make(chan struct{}, max)
	})
}

func acquire() { initInflight(); inflightSem <- struct{}{} }
func release() { <-inflightSem }

func browserExecCheck(ctx context.Context, url string) (bool, map[string]any) {
	if url == "" || serviceRegistry == nil {
		return false, map[string]any{"error": "missing_url_or_service_registry"}
	}
	// Host-level short cache to reduce duplicate checks
	host := hostOf(url)
	if host != "" {
		if ok, res, hit := lookupHostCache(host); hit {
			atomic.AddInt64(&cacheHits, 1)
			metricCacheHits.Inc()
			return ok, res
		}
		atomic.AddInt64(&cacheMiss, 1)
		metricCacheMiss.Inc()
		// Singleflight: coalesce parallel checks for same host
		if ch := joinSingleflight(host); ch != nil {
			r := <-ch
			return r.ok, r.res
		}
		// mark as leader; ensure notify on return
		defer func() { leaveSingleflight(host) }()
	}
	// Concurrency guard
	acquire()
	atomic.AddInt32(&inflightCur, 1)
	metricInflight.Inc()
	defer release()
	defer metricInflight.Dec()
	defer atomic.AddInt32(&inflightCur, -1)

	var out map[string]any
	err := serviceRegistry.CallJSON(ctx, "browser-exec", serviceclient.Request{
		Method: http.MethodPost,
		Path:   "/api/v1/browser/check-availability",
		Body: map[string]any{
			"url":       url,
			"timeoutMs": 8000,
		},
	}, &out)

	if err != nil {
		return false, map[string]any{"error": err.Error()}
	}
	ok := false
	if v, ok2 := out["ok"].(bool); ok2 {
		ok = v
	}
	// if 'ok' not provided, consider http-level 2xx success path already implied by CallJSON
	// fill cache
	if host != "" {
		ttlMs := 120000
		if !ok {
			ttlMs = 30000
		}
		if v := strings.TrimSpace(os.Getenv("BATCHOPEN_DOMAIN_CACHE_MS")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 1000 && n <= 600000 {
				ttlMs = n
			}
		}
		saveHostCache(host, ok, out, time.Duration(ttlMs)*time.Millisecond)
	}
	return ok, out
}

func browserExecCheckWithRetry(ctx context.Context, url string) (bool, map[string]any) {
	maxRetries := 2
	if v := strings.TrimSpace(os.Getenv("BATCHOPEN_RETRIES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 5 {
			maxRetries = n
		}
	}
	backoff := 300
	if v := strings.TrimSpace(os.Getenv("BATCHOPEN_BACKOFF_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 50 && n <= 5000 {
			backoff = n
		}
	}
	var last map[string]any
	for attempt := 0; ; attempt++ {
		ok, out := browserExecCheck(ctx, url)
		metricRequests.Inc()
		last = out
		if ok {
			return true, out
		}
		// decide retry by status
		status := 0
		if v, ok := out["status"].(float64); ok {
			status = int(v)
		}
		transient := status == 0 || status >= 500 || status == 429
		if !transient || attempt >= maxRetries {
			metricErrors.Inc()
			return false, out
		}
		wait := time.Duration(backoff*(1<<attempt)) * time.Millisecond
		if wait > 2*time.Second {
			wait = 2 * time.Second
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return false, last
		}
	}
}

func computeQuality(res map[string]any) (int, map[string]any) {
	status := 0
	if v, ok := res["status"].(float64); ok {
		status = int(v)
	}
	engine := ""
	if v, ok := res["engine"].(string); ok {
		engine = v
	}
	score := 0
	switch {
	case status >= 200 && status < 300:
		score = 90
	case status >= 300 && status < 400:
		score = 70
	case status >= 400 && status < 500:
		score = 30
	case status >= 500:
		score = 15
	default:
		score = 10
	}
	if engine == "playwright" {
		score += 5
	}
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	factors := map[string]any{"status": status, "engine": engine}
	return score, factors
}

// --- AutoClick minimal read model & tick executor ---

func ensureAutoClickDDL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS "AutoClickTask"(
  id            BIGSERIAL PRIMARY KEY,
  user_id       TEXT NOT NULL,
  url           TEXT NOT NULL,
  interval_min  INT  NOT NULL DEFAULT 60,
  enabled       BOOLEAN NOT NULL DEFAULT TRUE,
  next_run_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_run_at   TIMESTAMPTZ,
  status        TEXT NOT NULL DEFAULT 'idle',
  meta          JSONB,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ix_autoclick_user_next ON "AutoClickTask"(user_id, next_run_at);
CREATE TABLE IF NOT EXISTS "AutoClickExecution"(
  id           BIGSERIAL PRIMARY KEY,
  task_id      BIGINT NOT NULL,
  user_id      TEXT   NOT NULL,
  url          TEXT   NOT NULL,
  started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at  TIMESTAMPTZ,
  success      BOOLEAN,
  response     JSONB
);
`)
	return err
}

// autoClickTasksHandler handles:
// - POST /api/v1/batchopen/autoclick/tasks  { url, intervalMin?, fingerprint?, proxy? }
// - GET  /api/v1/batchopen/autoclick/tasks  -> list current user's tasks
func autoClickTasksHandler(w http.ResponseWriter, r *http.Request) {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		errors.Write(w, r, http.StatusFailedDependency, "MISSING_DB", "DATABASE_URL not configured", nil)
		return
	}
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "DB_OPEN_FAILED", "open db failed", nil)
		return
	}
	defer db.Close()
	if err := ensureAutoClickDDL(db); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "DDL_FAILED", "ensure ddl failed", nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.QueryContext(r.Context(), `SELECT id, url, interval_min, enabled, next_run_at, last_run_at, status, COALESCE(meta::text,'null') FROM "AutoClickTask" WHERE user_id=$1 ORDER BY id DESC LIMIT 200`, uid)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "QUERY_FAILED", "query failed", nil)
			return
		}
		defer rows.Close()
		type item struct {
			ID          int64           `json:"id"`
			Url         string          `json:"url"`
			IntervalMin int             `json:"intervalMin"`
			Enabled     bool            `json:"enabled"`
			NextRunAt   *time.Time      `json:"nextRunAt,omitempty"`
			LastRunAt   *time.Time      `json:"lastRunAt,omitempty"`
			Status      string          `json:"status"`
			Meta        json.RawMessage `json:"meta,omitempty"`
		}
		var items []item
		for rows.Next() {
			var it item
			var next, last sql.NullTime
			var meta string
			if err := rows.Scan(&it.ID, &it.Url, &it.IntervalMin, &it.Enabled, &next, &last, &it.Status, &meta); err != nil {
				continue
			}
			if next.Valid {
				it.NextRunAt = &next.Time
			}
			if last.Valid {
				it.LastRunAt = &last.Time
			}
			if meta != "" && meta != "null" {
				it.Meta = json.RawMessage(meta)
			}
			items = append(items, it)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	case http.MethodPost:
		var body struct {
			Url         string         `json:"url"`
			IntervalMin int            `json:"intervalMin"`
			Fingerprint map[string]any `json:"fingerprint"`
			Proxy       map[string]any `json:"proxy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid json", nil)
			return
		}
		url := strings.TrimSpace(body.Url)
		if url == "" {
			errors.Write(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "url required", nil)
			return
		}
		interval := body.IntervalMin
		if interval <= 0 {
			interval = 60
		}
		meta := map[string]any{}
		if body.Fingerprint != nil {
			meta["fingerprint"] = body.Fingerprint
		}
		if body.Proxy != nil {
			meta["proxy"] = body.Proxy
		}
		b, _ := json.Marshal(meta)
		var id int64
		err := db.QueryRowContext(r.Context(), `INSERT INTO "AutoClickTask"(user_id, url, interval_min, meta) VALUES ($1,$2,$3,$4::jsonb) RETURNING id`, uid, url, interval, string(b)).Scan(&id)
		if err != nil {
			errors.Write(w, r, http.StatusInternalServerError, "INSERT_FAILED", "insert failed", nil)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
		return
	default:
		errors.Write(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
		return
	}
}

// autoClickExecuteTickHandler processes due tasks and triggers Browser-Exec simulate-click.
// POST /api/v1/batchopen/autoclick/execute-tick?max=N
func autoClickExecuteTickHandler(w http.ResponseWriter, r *http.Request) {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		errors.Write(w, r, http.StatusFailedDependency, "MISSING_DB", "DATABASE_URL not configured", nil)
		return
	}
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "DB_OPEN_FAILED", "open db failed", nil)
		return
	}
	defer db.Close()
	if err := ensureAutoClickDDL(db); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "DDL_FAILED", "ensure ddl failed", nil)
		return
	}

	maxN := 5
	if v := r.URL.Query().Get("max"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 50 {
			maxN = n
		}
	}

	// Pick due tasks (enabled, next_run_at <= now) across current user only or global? Use current user scope for safety.
	rows, err := db.QueryContext(r.Context(), `SELECT id, url, interval_min, COALESCE(meta::text,'null') FROM "AutoClickTask" WHERE user_id=$1 AND enabled=TRUE AND next_run_at <= NOW() ORDER BY next_run_at ASC LIMIT $2`, uid, maxN)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "QUERY_FAILED", "query failed", nil)
		return
	}
	defer rows.Close()
	type task struct {
		id       int64
		url      string
		interval int
		meta     json.RawMessage
	}
	var tasks []task
	for rows.Next() {
		var t task
		var meta string
		if err := rows.Scan(&t.id, &t.url, &t.interval, &meta); err == nil {
			if meta != "" && meta != "null" {
				t.meta = json.RawMessage(meta)
			}
			tasks = append(tasks, t)
		}
	}
	if len(tasks) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"status": "idle", "processed": 0})
		return
	}

	be := strings.TrimRight(os.Getenv("BROWSER_EXEC_URL"), "/")
	if be == "" {
		errors.Write(w, r, http.StatusFailedDependency, "MISSING_BE", "BROWSER_EXEC_URL not configured", nil)
		return
	}
	tok := strings.TrimSpace(os.Getenv("BROWSER_INTERNAL_TOKEN"))
	hdr := map[string]string{"Content-Type": "application/json"}
	if tok != "" {
		hdr["X-Service-Token"] = tok
	}
	cli := httpx.New(6 * time.Second)
	processed := 0
	for _, t := range tasks {
		// Build request body with optional meta fields
		var fp, proxy map[string]any
		if len(t.meta) > 0 {
			_ = json.Unmarshal([]byte(t.meta), &struct {
				Fingerprint *map[string]any `json:"fingerprint"`
				Proxy       *map[string]any `json:"proxy"`
			}{&fp, &proxy})
		}
		body := map[string]any{"url": t.url}
		if fp != nil {
			body["fingerprint"] = fp
		}
		if proxy != nil {
			body["proxy"] = proxy
		}
		// Best-effort call simulate-click (202 expected)
		var out map[string]any
		err := cli.DoJSON(r.Context(), http.MethodPost, be+"/api/v1/browser/simulate-click", body, hdr, 1, &out)
		success := err == nil
		// Insert execution log
		resp := map[string]any{"ok": success}
		if !success {
			resp["error"] = err.Error()
		} else {
			resp["response"] = out
		}
		payload, _ := json.Marshal(resp)
		_, _ = db.ExecContext(r.Context(), `INSERT INTO "AutoClickExecution"(task_id, user_id, url, finished_at, success, response) VALUES ($1,$2,$3,NOW(),$4,$5::jsonb)`, t.id, uid, t.url, success, string(payload))
		// Update task next/last
		_, _ = db.ExecContext(r.Context(), `UPDATE "AutoClickTask" SET last_run_at=NOW(), next_run_at=NOW() + ($1 || ' minutes')::interval, updated_at=NOW(), status=$3 WHERE id=$2`, strconv.Itoa(t.interval), t.id, func() string {
			if success {
				return "ok"
			}
			return "error"
		}())
		processed++
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "processed": processed})
}

// autoClickAnalysisHandler computes authenticity metrics for recent executions.
// GET /api/v1/batchopen/autoclick/analysis?days=7
func autoClickAnalysisHandler(w http.ResponseWriter, r *http.Request) {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		errors.Write(w, r, http.StatusFailedDependency, "MISSING_DB", "DATABASE_URL not configured", nil)
		return
	}
	uid, _ := r.Context().Value(middleware.UserIDKey).(string)
	if uid == "" {
		errors.Write(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized", nil)
		return
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "DB_OPEN_FAILED", "open db failed", nil)
		return
	}
	defer db.Close()
	if err := ensureAutoClickDDL(db); err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "DDL_FAILED", "ensure ddl failed", nil)
		return
	}
	days := 7
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 30 {
			days = n
		}
	}
	rows, err := db.QueryContext(r.Context(), `SELECT success, finished_at, COALESCE(response::text,'') FROM "AutoClickExecution" WHERE user_id=$1 AND finished_at > NOW() - ($2::text||' days')::interval ORDER BY finished_at DESC LIMIT 5000`, uid, strconv.Itoa(days))
	if err != nil {
		errors.Write(w, r, http.StatusInternalServerError, "QUERY_FAILED", "query failed", nil)
		return
	}
	defer rows.Close()
	total, succ := 0, 0
	var navSum, clickSum, navCnt, clickCnt int64
	hist := make([]int, 24)
	for rows.Next() {
		var ok bool
		var ts time.Time
		var resp string
		if err := rows.Scan(&ok, &ts, &resp); err != nil {
			continue
		}
		total++
		if ok {
			succ++
		}
		h := ts.UTC().Hour()
		hist[h]++
		if resp != "" && resp != "null" {
			var m map[string]any
			if json.Unmarshal([]byte(resp), &m) == nil {
				// response may be { ok, response:{ timings:{navMs,clickMs}} } or { ok, timings:{} }
				var timings map[string]any
				if v, ok := m["response"]; ok {
					if mm, ok := v.(map[string]any); ok {
						if t, ok := mm["timings"].(map[string]any); ok {
							timings = t
						}
					}
				}
				if timings == nil {
					if t, ok := m["timings"].(map[string]any); ok {
						timings = t
					}
				}
				if timings != nil {
					if v, ok := timings["navMs"].(float64); ok && v > 0 {
						navSum += int64(v)
						navCnt++
					}
					if v, ok := timings["clickMs"].(float64); ok && v > 0 {
						clickSum += int64(v)
						clickCnt++
					}
				}
			}
		}
	}
	mean := 0.0
	if total > 0 {
		mean = float64(total) / 24.0
	}
	var varSum float64
	for i := 0; i < 24; i++ {
		d := float64(hist[i]) - mean
		varSum += d * d
	}
	variance := varSum / 24.0
	cv := 0.0
	if mean > 0 {
		cv = (math.Sqrt(variance) / mean)
	}
	dispersionScore := math.Max(0, 100-math.Min(100, cv*100))
	successRate := 0.0
	if total > 0 {
		successRate = float64(succ) / float64(total)
	}
	successScore := successRate * 100.0
	avgNav := 0.0
	if navCnt > 0 {
		avgNav = float64(navSum) / float64(navCnt)
	}
	timingScore := 0.0
	if avgNav > 0 {
		// 目标区间~1500ms，偏移越大分越低
		timingScore = 100 - math.Min(100, math.Abs(avgNav-1500.0)/1500.0*100)
	}
	authenticity := math.Round(0.5*successScore + 0.3*dispersionScore + 0.2*timingScore)
	// suggestions
	var suggestions []map[string]string
	if successRate < 0.8 {
		suggestions = append(suggestions, map[string]string{"code": "suggest.proxy", "text": "成功率偏低，建议检查代理质量或适当重试/退避"})
	}
	if dispersionScore < 70 {
		suggestions = append(suggestions, map[string]string{"code": "suggest.schedule", "text": "小时分布不均，建议调整模型或增加随机扰动"})
	}
	if timingScore < 60 {
		suggestions = append(suggestions, map[string]string{"code": "suggest.timing", "text": "页面加载过快或过慢，适当调整 fingerprint/等待策略"})
	}
	// build histogram output
	hitems := make([]map[string]any, 0, 24)
	for i := 0; i < 24; i++ {
		hitems = append(hitems, map[string]any{"hour": i, "count": hist[i]})
	}
	out := map[string]any{
		"days":        days,
		"total":       total,
		"success":     succ,
		"successRate": successRate,
		"avgNavMs":    avgNav,
		"avgClickMs": func() float64 {
			if clickCnt > 0 {
				return float64(clickSum) / float64(clickCnt)
			}
			return 0
		}(),
		"histogram":   hitems,
		"scores":      map[string]any{"success": successScore, "dispersion": dispersionScore, "timing": timingScore, "authenticity": authenticity},
		"suggestions": suggestions,
	}
	writeJSON(w, http.StatusOK, out)
}

// billingAction calls billing service reserve/commit/release for the task (best-effort, 2s timeout).
// read remote token config: token.batchopen.costPerUrl (fallback to env BATCHOPEN_TOKENS_PER_TASK)
func loadBatchopenTokenCost(ctx context.Context) int {
	// default
	amount := 1
	if v := strings.TrimSpace(os.Getenv("BATCHOPEN_TOKENS_PER_TASK")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			amount = n
		}
	}
	base := strings.TrimSpace(os.Getenv("CONSOLE_URL"))
	if base == "" {
		return amount
	}
	// fetch ops config snapshot
	url := strings.TrimRight(base, "/") + "/ops/console/config/v1"
	ctx2, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	var snap struct {
		Config map[string]any `json:"config"`
	}
	if err := httpx.New(1200*time.Millisecond).DoJSON(ctx2, http.MethodGet, url, nil, nil, 1, &snap); err != nil || snap.Config == nil {
		return amount
	}
	// navigate token.batchopen.costPerUrl
	t0, ok := snap.Config["token"].(map[string]any)
	if !ok {
		return amount
	}
	b0, ok := t0["batchopen"].(map[string]any)
	if !ok {
		return amount
	}
	if v, ok := b0["costPerUrl"]; ok {
		switch x := v.(type) {
		case float64:
			if x > 0 {
				return int(x)
			}
		case int:
			if x > 0 {
				return x
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil && n > 0 {
				return n
			}
		}
	}
	return amount
}

func billingAction(ctx context.Context, userID, action, taskID string) error {
	if userID == "" || taskID == "" || serviceRegistry == nil {
		return nil
	}
	amount := loadBatchopenTokenCost(ctx)

	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	body := map[string]any{
		"amount":      amount,
		"taskId":      taskID,
		"description": fmt.Sprintf("batchopen:%s:%s", taskID, action),
		"metadata":    map[string]any{"kind": "batchopen", "taskId": taskID, "action": action},
	}
	// For commit/release, allow idempotent txId to be taskID
	if action == "commit" || action == "release" {
		body["txId"] = taskID
	}

	_ = serviceRegistry.CallJSON(cctx, "billing", serviceclient.Request{
		Method: http.MethodPost,
		Path:   "/api/v1/billing/tokens/" + action,
		Body:   body,
		Headers: map[string]string{
			"X-User-Id":         userID,
			"X-Idempotency-Key": "batchopen:" + action + ":" + userID + ":" + taskID,
		},
	}, nil)

	return nil
}

// helpers for host cache and singleflight
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		h := strings.ToLower(u.Hostname())
		if h == "" {
			return ""
		}
		if strings.HasPrefix(h, "www.") {
			h = h[4:]
		}
		return h
	}
	return ""
}
func lookupHostCache(host string) (bool, map[string]any, bool) {
	hostCacheMu.RLock()
	e, ok := hostCache[host]
	hostCacheMu.RUnlock()
	if !ok || time.Now().After(e.exp) {
		return false, nil, false
	}
	return e.ok, e.res, true
}
func saveHostCache(host string, ok bool, res map[string]any, ttl time.Duration) {
	hostCacheMu.Lock()
	hostCache[host] = hostCacheEntry{ok: ok, res: res, exp: time.Now().Add(ttl)}
	hostCacheMu.Unlock()
}
func joinSingleflight(host string) <-chan singleflightRes {
	sfMu.Lock()
	defer sfMu.Unlock()
	if chs, ok := sfWaiters[host]; ok {
		ch := make(chan singleflightRes, 1)
		sfWaiters[host] = append(chs, ch)
		return ch
	}
	// mark leader by creating empty slice
	sfWaiters[host] = []chan singleflightRes{}
	return nil
}
func leaveSingleflight(host string) {
	sfMu.Lock()
	chs, ok := sfWaiters[host]
	if ok {
		delete(sfWaiters, host)
	}
	sfMu.Unlock()
	if !ok || len(chs) == 0 {
		return
	}
	// read from cache (which leader should have saved), otherwise send failure
	ok2, res, hit := lookupHostCache(host)
	r := singleflightRes{ok: ok2, res: res}
	if !hit {
		r = singleflightRes{ok: false, res: map[string]any{"error": "singleflight_miss"}}
	}
	for _, ch := range chs {
		ch <- r
		close(ch)
	}
}
