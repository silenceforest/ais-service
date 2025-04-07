package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xitongsys/parquet-go-source/local"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

// AISSTREAM_URL specifies the WebSocket endpoint for real-time AIS data stream.
const AISSTREAM_URL = "wss://stream.aisstream.io/v0/stream"

// FLUSH_INTERVAL defines how frequently (in wall-clock time) the buffered data is persisted to disk.
const FLUSH_INTERVAL = 60 * time.Minute

// MaxRecordsPerFile sets the upper limit of buffered AIS records per Parquet file before a flush is triggered.
const MaxRecordsPerFile = 100000

// Global state: protected buffer and counters
var (
	mu             sync.Mutex // Ensures safe concurrent access to in-memory buffer
	recordCount    int        // Tracks the number of buffered records
	lastLogPercent int        // Used to log buffer progress in 10% increments
)

// AISRecord defines the in-memory structure of a single AIS message,
// and the schema for writing to Apache Parquet format using `parquet-go` tags.
type AISRecord struct {
	Timestamp string `parquet:"name=timestamp, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN"`
	MMSI      string `parquet:"name=mmsi, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN"`
	RawJSON   string `parquet:"name=raw_json, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN"`
}

// aisRecords acts as an in-memory buffer accumulating raw AIS messages for periodic batch write.
var aisRecords []AISRecord

// runCollector initializes the AIS data collection process:
// - Connects to a WebSocket stream
// - Buffers messages in memory
// - Periodically flushes to disk in Parquet format
func runCollector(apiKey string, db *sql.DB, ctx context.Context) {
	log.Println("Starting AIS Data Collector...")

	// Ensure output directory exists
	if err := os.MkdirAll("ais_data", os.ModePerm); err != nil {
		log.Fatal("Failed to create directory:", err)
	}

	// Launch non-blocking WebSocket listener
	go connectWebSocket(apiKey, db)

	// Start periodic flush cycle
	ticker := time.NewTicker(FLUSH_INTERVAL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping AIS Data Collector...")
			return
		case <-ticker.C:
			saveToParquet(db)
		}
	}
}

// connectWebSocket manages the persistent WebSocket connection to the AIS stream provider.
// On connection loss, it retries automatically with exponential backoff.
// Subscribes to AIS messages using predefined bounding box.
func connectWebSocket(apiKey string, db *sql.DB) {
	for {
		log.Println("Connecting to AIS WebSocket...")

		conn, _, err := websocket.DefaultDialer.Dial(AISSTREAM_URL, nil)
		if err != nil {
			log.Println("WebSocket connection error:", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Prepare subscription payload with API key and bounding box filter
		subscription := map[string]interface{}{
			"APIKey":        apiKey,
			"BoundingBoxes": AtlanticAndMediterraneanBoundingBox,
		}
		subData, _ := json.Marshal(subscription)

		if err = conn.WriteMessage(websocket.TextMessage, subData); err != nil {
			log.Println("Subscription error:", err)
			conn.Close()
			continue
		}

		log.Println("Listening for AIS messages...")

		// Main receiving loop
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("WebSocket read error:", err)
				conn.Close()
				break
			}
			handleAISMessage(message, db)
		}

		// Reconnect after disconnect
		time.Sleep(2 * time.Second)
	}
}

// handleAISMessage decodes incoming raw AIS JSON messages,
// extracts the MMSI identifier, and stores them in the buffer.
//
// Automatically triggers a flush when the record limit is reached.
func handleAISMessage(message []byte, db *sql.DB) {
	var data map[string]interface{}
	err := json.Unmarshal(message, &data)
	if err != nil {
		log.Println("JSON parse error:", err)
		return
	}

	mmsi := extractMMSI(data)
	if mmsi == "" {
		return
	}

	record := AISRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		MMSI:      mmsi,
		RawJSON:   string(message),
	}

	mu.Lock()
	aisRecords = append(aisRecords, record)
	recordCount++
	mu.Unlock()

	// Log buffer progress in 10% steps
	currentPercent := (recordCount * 100) / MaxRecordsPerFile
	if currentPercent/10 != lastLogPercent/10 {
		log.Printf("Buffer fill: %d%% (%d/%d)\n", currentPercent, recordCount, MaxRecordsPerFile)
		lastLogPercent = currentPercent
	}

	// Auto-flush on capacity
	if recordCount >= MaxRecordsPerFile {
		saveToParquet(db)
	}
}

// extractMMSI traverses the nested AIS JSON payload and extracts the numeric UserID (MMSI).
// Returns empty string if the expected fields are missing or malformed.
func extractMMSI(data map[string]interface{}) string {
	msg, ok := data["Message"].(map[string]interface{})
	if !ok {
		return ""
	}
	pos, ok := msg["PositionReport"].(map[string]interface{})
	if !ok {
		return ""
	}
	mmsi, ok := pos["UserID"].(float64)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.0f", mmsi)
}

// saveToParquet serializes the current in-memory buffer of AIS records
// into a compressed Parquet file using ZSTD codec.
//
// Uses parquet-go with local file output backend.
func saveToParquet(db *sql.DB) {
	log.Println("Locking in-memory buffer for Parquet save process")
	mu.Lock()
	defer mu.Unlock()

	if recordCount == 0 {
		log.Println("No data to save, skipping Parquet write.")
		return
	}

	currentFile := getNewFilePath()
	log.Printf("Saving %d records to Parquet file: %s", recordCount, currentFile)

	// Create local Parquet file writer
	fw, err := local.NewLocalFileWriter(currentFile)
	if err != nil {
		log.Println("Failed to create Parquet file writer:", err)
		return
	}

	pw, err := writer.NewParquetWriter(fw, new(AISRecord), 4)
	if err != nil {
		log.Println("Error initializing Parquet writer:", err)
		return
	}
	pw.CompressionType = parquet.CompressionCodec_ZSTD

	startTime := time.Now()
	for _, rec := range aisRecords {
		if err = pw.Write(rec); err != nil {
			log.Println("Parquet write error:", err)
		}
	}
	if err = pw.WriteStop(); err != nil {
		log.Println("Error finalizing Parquet writer:", err)
		return
	}

	log.Printf("Parquet file %s written successfully in %.2f seconds.", currentFile, time.Since(startTime).Seconds())

	// Report file size
	if fileInfo, err := os.Stat(currentFile); err == nil {
		log.Printf("Parquet file size: %.2f MB", float64(fileInfo.Size())/1024/1024)
	} else {
		log.Println("Could not retrieve file size:", err)
	}

	// Reset state after successful save
	aisRecords = nil
	recordCount = 0
	log.Println("In-memory buffer cleared after Parquet save.")
}

// getNewFilePath returns a timestamped Parquet filename in `ais_data/` directory.
// Format: ais_data/YYYY-MM-DD_HH-MM-SS.parquet
func getNewFilePath() string {
	return fmt.Sprintf("ais_data/%s.parquet", time.Now().Format("2006-01-02_15-04-05"))
}
