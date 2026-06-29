package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	ff "github.com/GoogleCloudPlatform/functions-framework-go/functions"
)

// Pub/Sub message structure
type PubSubMessage struct {
	Data []byte `json:"data"`
}

// DispatchPayload defines the HTTP dispatch instruction carried in Pub/Sub data
type DispatchPayload struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	Body      any               `json:"body"`
	TimeoutMs int               `json:"timeoutMs"`
}

func init() { ff.CloudEvent("Dispatch", dispatch) }

func dispatch(ctx context.Context, msg PubSubMessage) error {
	if len(msg.Data) == 0 {
		return errors.New("empty pubsub data")
	}
	var p DispatchPayload
	if err := json.Unmarshal(msg.Data, &p); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}
	if strings.TrimSpace(p.URL) == "" {
		return errors.New("missing url")
	}
	if strings.TrimSpace(p.Method) == "" {
		p.Method = http.MethodPost
	}
	// body
	var bodyReader io.Reader
	if p.Body != nil {
		b, _ := json.Marshal(p.Body)
		bodyReader = strings.NewReader(string(b))
	}
	// headers
	hdr := http.Header{}
	for k, v := range p.Headers {
		hdr.Set(k, v)
	}
	if hdr.Get("Content-Type") == "" {
		hdr.Set("Content-Type", "application/json")
	}
	// Inject INTERNAL_SERVICE_TOKEN if requested
	if strings.EqualFold(hdr.Get("X-Service-Token"), "ENV") {
		tok := strings.TrimSpace(os.Getenv("INTERNAL_SERVICE_TOKEN"))
		if tok != "" {
			hdr.Set("X-Service-Token", tok)
		}
	}
	// timeout
	timeout := time.Duration(p.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > 25*time.Second {
		timeout = 5 * time.Second
	}
	req, _ := http.NewRequestWithContext(ctx, p.Method, p.URL, bodyReader)
	req.Header = hdr
	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		log.Printf("dispatch non-2xx: code=%d body=%s", resp.StatusCode, string(b))
		return fmt.Errorf("non-2xx: %d", resp.StatusCode)
	}
	return nil
}
