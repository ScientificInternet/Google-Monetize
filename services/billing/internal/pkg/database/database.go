package database

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq" // The PostgreSQL driver
)

// NewConnection creates and returns a new database connection pool.
// It takes a Data Source Name (DSN) string as input.
func NewConnection(dsn string) (*sql.DB, error) {
	// sql.Open just validates its arguments without creating a connection.
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// db.Ping verifies a connection to the database is still alive,
	// establishing a connection if necessary.
	if err = db.Ping(); err != nil {
		// If ping fails, close the connection pool and return the error.
		defer db.Close()
		return nil, fmt.Errorf("failed to connect to the database: %w", err)
	}

	return db, nil
}
