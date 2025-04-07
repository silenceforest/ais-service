// main.go

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/marcboeker/go-duckdb" // DuckDB driver for Go's database/sql
)

var db *sql.DB

// runAPI initializes the HTTP server with RESTful routes using Gin,
// configures middleware for contextual time-based filtering,
// and launches the server as a non-blocking goroutine.
//
// Graceful shutdown is ensured by monitoring context cancellation.
func runAPI(db *sql.DB, ctx context.Context) {
	router := gin.Default()

	// Global middleware to extract 'from'/'to' date parameters from the query string
	router.Use(func(c *gin.Context) {
		middlewareDateRange(c)
	})

	// === API ROUTES ===

	// Example: GET /ships/273450000?from=2023-09-01&to=2023-09-03
	// Returns all AIS messages for a given MMSI over a date range
	router.GET("/ships/:mmsi", func(c *gin.Context) { getShipData(c, db) })

	// Example: GET /ships/mmsi?from=2023-09-01
	// Returns a distinct list of all MMSIs present in the dataset for given date(s)
	router.GET("/ships/mmsi", func(c *gin.Context) { getUniqueMMSI(c, db) })

	// Example: GET /latest?from=2023-09-01
	// Returns the 10 most recent messages across all ships
	router.GET("/latest", func(c *gin.Context) { getLatestAllShips(c, db) })

	// Example: GET /stats?from=2023-09-01
	// Returns summary statistics (total messages, frequency, etc.)
	router.GET("/stats", func(c *gin.Context) { getStats(c, db) })

	// Run HTTP server asynchronously
	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server error: %s", err)
		}
	}()

	// Wait for cancellation (e.g., SIGINT) to gracefully stop the server
	<-ctx.Done()
	log.Println("Shutting down API server...")
	server.Shutdown(context.Background())
}

// middlewareDateRange parses the 'from' and 'to' date parameters (ISO 8601, e.g., 2023-09-01),
// defaults to the current UTC day if not provided, and finds matching Parquet files by date.
//
// These values are injected into the Gin context for downstream handlers.
// In case of invalid input or absence of data files, request is aborted with HTTP 4xx/5xx status.
func middlewareDateRange(c *gin.Context) {
	layout := "2006-01-02"

	fromStr := c.DefaultQuery("from", time.Now().UTC().Format(layout))
	toStr := c.DefaultQuery("to", fromStr)

	from, err := time.Parse(layout, fromStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'from' date format (expected YYYY-MM-DD)"})
		c.Abort()
		return
	}

	to, err := time.Parse(layout, toStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid 'to' date format (expected YYYY-MM-DD)"})
		c.Abort()
		return
	}

	files := getFilePaths(fromStr, toStr)
	if len(files) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No data available for the requested period"})
		c.Abort()
		return
	}

	// Store date range and matched files into context
	c.Set("from", from)
	c.Set("to", to)
	c.Set("files", files)

	log.Printf("API Request: from=%s, to=%s, files=%v\n", fromStr, toStr, files)
	c.Next()
}

// getFilePaths returns all Parquet file paths under 'ais_data/' that match the given date range.
// Expected file format: ais_data/YYYY-MM-DD_*.parquet
func getFilePaths(from, to string) []string {
	var files []string
	layout := "2006-01-02"

	start, _ := time.Parse(layout, from)
	end, _ := time.Parse(layout, to)

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		pattern := fmt.Sprintf("ais_data/%s_*.parquet", d.Format(layout))
		matches, _ := filepath.Glob(pattern)
		files = append(files, matches...)
	}

	return files
}

// getShipData queries all messages from Parquet files for a specific MMSI (Maritime Mobile Service Identity).
//
// DuckDB reads multiple Parquet files via `read_parquet(ARRAY[...])` syntax,
// and a WHERE clause is applied to filter rows by MMSI.
//
// Example: GET /ships/273450000?from=2023-09-01&to=2023-09-03
func getShipData(c *gin.Context, db *sql.DB) {
	mmsi := c.Param("mmsi")
	files := c.MustGet("files").([]string)

	fileList := "ARRAY['" + strings.Join(files, "', '") + "']"
	query := fmt.Sprintf(`
		SELECT timestamp, mmsi, raw_json 
		FROM read_parquet(%s) 
		WHERE mmsi = ?`, fileList)

	rows, err := db.Query(query, mmsi)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var timestamp string
		var mmsi int
		var rawData string
		rows.Scan(&timestamp, &mmsi, &rawData)
		results = append(results, gin.H{
			"timestamp": timestamp,
			"mmsi":      mmsi,
			"raw_data":  rawData,
		})
	}
	c.JSON(http.StatusOK, results)
}

// getUniqueMMSI returns a list of all unique MMSI values present in the selected files.
//
// Example: GET /ships/mmsi?from=2023-09-01
func getUniqueMMSI(c *gin.Context, db *sql.DB) {
	files := c.MustGet("files").([]string)

	fileList := "ARRAY['" + strings.Join(files, "', '") + "']"
	query := fmt.Sprintf("SELECT DISTINCT mmsi FROM read_parquet(%s)", fileList)

	rows, err := db.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var mmsis []int
	for rows.Next() {
		var mmsi int
		rows.Scan(&mmsi)
		mmsis = append(mmsis, mmsi)
	}
	c.JSON(http.StatusOK, mmsis)
}

// getLatestAllShips returns the most recent N AIS messages (hardcoded: 10),
// sorted in descending order by timestamp.
//
// Example: GET /latest?from=2023-09-01
func getLatestAllShips(c *gin.Context, db *sql.DB) {
	files := c.MustGet("files").([]string)

	fileList := "ARRAY['" + strings.Join(files, "', '") + "']"
	query := fmt.Sprintf(`
		SELECT timestamp, mmsi, raw_json 
		FROM read_parquet(%s) 
		ORDER BY timestamp DESC 
		LIMIT 10`, fileList)

	rows, err := db.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var timestamp string
		var mmsi int
		var rawData string
		rows.Scan(&timestamp, &mmsi, &rawData)
		results = append(results, gin.H{
			"timestamp": timestamp,
			"mmsi":      mmsi,
			"raw_data":  rawData,
		})
	}
	c.JSON(http.StatusOK, results)
}

// getStats provides summary statistics on message frequency within the queried files.
//
// It computes:
// - total message count
// - message count for last 60 minutes and last 1 minute (based on NOW())
// - average number of messages per minute (assuming 1440 minutes/day)
//
// Example: GET /stats?from=2023-09-01
func getStats(c *gin.Context, db *sql.DB) {
	files := c.MustGet("files").([]string)
	fileList := "ARRAY['" + strings.Join(files, "', '") + "']"

	queryTotal := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet(%s)", fileList)
	queryLastHour := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet(%s) WHERE timestamp >= CAST(NOW() AS TIMESTAMP) - INTERVAL '1 hour'", fileList)
	queryLastMinute := fmt.Sprintf("SELECT COUNT(*) FROM read_parquet(%s) WHERE timestamp >= CAST(NOW() AS TIMESTAMP) - INTERVAL '1 minute'", fileList)
	queryAvgPerMinute := fmt.Sprintf("SELECT CAST(COUNT(*) / 1440 AS INTEGER) FROM read_parquet(%s)", fileList)

	var total, lastHour, lastMinute, avgPerMinute int

	err := db.QueryRow(queryTotal).Scan(&total)
	if err != nil {
		log.Println("Query total_today error:", err)
	}

	err = db.QueryRow(queryLastHour).Scan(&lastHour)
	if err != nil {
		log.Println("Query last_hour error:", err)
	}

	err = db.QueryRow(queryLastMinute).Scan(&lastMinute)
	if err != nil {
		log.Println("Query last_minute error:", err)
	}

	err = db.QueryRow(queryAvgPerMinute).Scan(&avgPerMinute)
	if err != nil {
		log.Println("Query avg_per_minute error:", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"total_today":        total,
		"last_hour":          lastHour,
		"last_minute":        lastMinute,
		"average_per_minute": avgPerMinute,
	})
}
