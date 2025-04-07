package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/marcboeker/go-duckdb" // DuckDB SQL driver
)

// newDBConnection initializes a new DuckDB connection in in-memory mode.
// DuckDB operates on a per-connection basis, so separate instances are used
// for parallel workloads (collector and API).
func newDBConnection() (*sql.DB, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("DuckDB connection failed: %w", err)
	}
	return db, nil
}

func main() {
	// === Environment Validation ===

	// AIS_API_KEY is required for authenticating with the AIS WebSocket stream.
	apiKey := os.Getenv("AIS_API_KEY")
	if apiKey == "" {
		log.Fatal("AIS_API_KEY is required")
	}

	fmt.Println("Starting AIS Service...")

	// === Graceful Shutdown Setup ===

	// Create a context that listens for system interrupt or termination signals.
	// This allows for clean resource shutdown (e.g., closing DB, stopping goroutines).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// === Database Connections ===

	// Create a dedicated DuckDB connection for serving API requests.
	apiDB, err := newDBConnection()
	if err != nil {
		log.Fatal(err)
	}
	defer apiDB.Close()

	// Create a second DuckDB connection for the data collector (write-only).
	// This separation allows concurrent read/write without locking.
	collectorDB, err := newDBConnection()
	if err != nil {
		log.Fatal(err)
	}
	defer collectorDB.Close()

	// === Concurrency Coordination ===

	// Create a channel to synchronize the collector goroutine shutdown.
	done := make(chan struct{})

	// Launch the AIS collector as a background goroutine.
	go func() {
		defer close(done)
		runCollector(apiKey, collectorDB, ctx)
	}()

	// Allow the collector a short startup period before launching the API.
	time.Sleep(2 * time.Second)

	// Launch the HTTP API in another background goroutine.
	go runAPI(apiDB, ctx)

	// === Await Termination Signal ===

	// Block main thread until the user sends SIGINT or SIGTERM.
	<-ctx.Done()
	fmt.Println("Shutting down AIS Service...")

	// Wait for the collector to finish cleanup.
	<-done
	fmt.Println("Service stopped.")
}
