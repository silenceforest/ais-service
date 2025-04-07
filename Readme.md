**AIS Real-Time Data Pipeline with DuckDB, Gin & WebSocket**  
**Self-contained system for high-frequency maritime telemetry capture and local analytics**

> This project implements a lightweight, zero-dependency backend for capturing and analyzing Automatic Identification System (AIS) data in real time.  
>
> Its core function is to **persistently record all AIS messages within a rolling one-hour window** into highly compressed Parquet files (~30–40 MB per hour), enabling efficient local querying and retrospective analysis.  
>
> Built in Go, it leverages an embedded DuckDB engine for in-memory SQL analytics, a WebSocket client for real-time ingestion, and the Gin web framework for a simple API interface.

---

## ⚙️ Overview

This system ingests AIS (Automatic Identification System) data in real time from a public WebSocket stream, buffers it in memory, stores batches in compressed Parquet format, and exposes an HTTP API for querying ship data via DuckDB.

It is a **self-contained, zero-dependency analytics backend**, suitable for:

- research on maritime traffic,
- offline data science pipelines,
- real-time traffic monitoring.

---

## 🧱 Architecture

```
[ AIS WebSocket Stream ] 
          │
          ▼
  ┌─────────────────────┐
  │ Collector (Go)      │
  │ - WebSocket client  │
  │ - Bounding box      │
  │ - ZSTD Parquet write│
  └─────────────────────┘
          │
          ▼
[ ais_data/YYYY-MM-DD_HH-MM-SS.parquet ]
          ▲
          │
  ┌─────────────────────┐
  │ API Server (Gin)    │
  │ - DuckDB SQL engine │
  │ - RESTful interface │
  └─────────────────────┘
```

---

## 📂 File Structure

```
.
├── main.go           # Entry point (context, signals)
├── api.go            # HTTP API handlers using Gin + DuckDB
├── collector.go      # Launches AIS collector goroutine
├── stream.go         # WebSocket buffer & Parquet writing
├── boxs.go           # Bounding box definitions
├── Makefile          # Deployment and build automation
├── go.mod / go.sum   # Go module definitions
├── README.md         # This file
└── ais_data/         # Output Parquet files (auto-created)
```

---

## 🚀 Usage

### 1. Requirements

- Go 1.21+
- Valid API key from [aisstream.io](https://aisstream.io)
- Optional: remote server with SSH access

### 2. Environment

Set the API key:

```bash
export AIS_API_KEY=your_key_here
```

### 3. Run Locally

Build and launch:

```bash
make build
./ais_service
```

Parquet files will be saved to `./ais_data/`, API will be available at `http://localhost:8080`.

---

## 🌐 API Endpoints

All endpoints accept `?from=YYYY-MM-DD[&to=YYYY-MM-DD]` query params.

### `GET /ships/:mmsi`
Returns all messages for the given MMSI.

```http
GET /ships/273450000?from=2023-09-01&to=2023-09-03
```

### `GET /ships/mmsi`
List all unique MMSIs in selected files.

```http
GET /ships/mmsi?from=2023-09-01
```

### `GET /latest`
Returns the 10 most recent AIS messages.

```http
GET /latest?from=2023-09-01
```

### `GET /stats`
Returns:
- Total messages
- Last hour / last minute counts
- Average messages per minute

```http
GET /stats?from=2023-09-01
```

---

## 📦 Deployment

You can deploy the system to a remote Linux server via `make deploy`.

### Required Variables

In your shell:

```bash
export SERVER=user@remote-host
export DEST_DIR=~/ais_service
```

### Run Deployment

```bash
make deploy
```

What it does:

- Uses `rsync` to transfer source files to the remote server (excluding binary, data, .git)
- Runs `make build-linux` remotely to build for Linux
- Cleans up build artifacts

---

## 🧰 Makefile Commands

| Command        | Description                                      |
|----------------|--------------------------------------------------|
| `make build`   | Build the binary for current OS/arch             |
| `make build-linux` | Cross-compile for Linux (CGO enabled)       |
| `make deploy`  | Rsync to remote server and build there           |

---

## 🧪 Parquet Output Schema

```go
type AISRecord struct {
	Timestamp string `parquet:"name=timestamp, type=BYTE_ARRAY, convertedtype=UTF8"`
	MMSI      string `parquet:"name=mmsi, type=BYTE_ARRAY, convertedtype=UTF8"`
	RawJSON   string `parquet:"name=raw_json, type=BYTE_ARRAY, convertedtype=UTF8"`
}
```

Each record includes ISO 8601 timestamp, MMSI ID, and original JSON payload.

---

## 💡 Highlights

- 🌐 DuckDB allows SQL queries directly over compressed Parquet files
- 🔐 Fully self-contained, works offline
- 🧵 Concurrent design: isolated DBs for writer and reader
- 🧠 Supports bounding box filtering at WebSocket subscription level

---

## 📜 License

MIT License — free to use for commercial or academic purposes.

---

Feel free to contribute by opening issues or submitting pull requests.
For any questions or suggestions, please contact me via https://andrewn.name
