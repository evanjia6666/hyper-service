# Hyper Service

A Go service that monitors hypervisor node data files and provides real-time WebSocket updates for whitelisted users, with PostgreSQL storage and Redis integration.

## Features

- Real-time monitoring of log-style data files (fills and misc events)
- WebSocket server with user-specific subscription support
- Whitelist-based filtering of events using Bloom filter
- Automatic hourly file switching
- PostgreSQL storage for event data with time-based cleanup (1 week retention)
- Redis integration for Bloom filter initialization and file position tracking
- API for querying events by user address, event type, and block range
- Docker Compose setup with Redis, PostgreSQL, and Adminer web UI

## Prerequisites

- Go 1.24 or later
- Docker and Docker Compose (for containerized deployment)
- Bash shell for running test scripts

## Installation

```bash
go mod tidy
```

## Usage

### Running with Docker Compose (Recommended)

1. Start all services:
   ```bash
   docker-compose up -d
   ```

2. The services will be available at:
   - Hyper Service WebSocket: `ws://localhost:8080/ws`
   - Hyper Service HTTP API: `http://localhost:8000`
   - PostgreSQL Adminer UI: `http://localhost:8081`
   - Redis: `localhost:6379`
   - PostgreSQL: `localhost:5432`

### Running the Service Directly

```bash
go run cmd/main.go -data_dir=/path/to/data -whitelist=configs/whitelist.txt -port=8080
```

Parameters:
- `data_dir`: Path to the directory containing node data files
- `whitelist`: Path to the whitelist file (default: configs/whitelist.txt)
- `port`: Port for the WebSocket server (default: 8000)
- `redis_addr`: Redis server address (default: localhost:6379)
- `postgres_addr`: PostgreSQL server address (default: localhost:5432)
- `postgres_user`: PostgreSQL user (default: postgres)
- `postgres_password`: PostgreSQL password (default: postgres)
- `postgres_db`: PostgreSQL database name (default: hyper_service)

### Testing with Sample Data

1. Generate test data:
   ```bash
   ./scripts/generate_test_data.sh /tmp/test-data
   ```

2. In another terminal, run the service:
   ```bash
   go run cmd/main.go -data_dir=/tmp/test-data -port=8080
   ```

3. Open `scripts/test_client.html` in a browser to connect to the WebSocket and view events.

### WebSocket API

The WebSocket server supports subscription management:

- Subscribe to events for all whitelisted users:
  ```json
  {
    "action": "subscribe",
    "event": "fill"
  }
  ```

- Unsubscribe from events:
  ```json
  {
    "action": "unsubscribe",
    "event": "fill"
  }
  ```

### HTTP Endpoints

- Whitelist management: `http://localhost:8080/whitelist` (GET to retrieve, POST to reload)
- Event query API: `http://localhost:8080/events` (POST with JSON query)

Event query example:
```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '{
    "userAddress": "0x348e5365acfa48a26ada7da840ca611e29c950ef",
    "event": "fill",
    "startBlock": 696372899,
    "endBlock": 696374333
  }'
```

## Project Structure

```
.
├── cmd/
│   └── main.go          # Main application entry point
├── configs/
│   ├── config.go        # Configuration structure
│   └── whitelist.txt    # Whitelisted user addresses
├── internal/
│   └── service/
│       ├── service.go   # Core service implementation
│       └── schema.sql   # PostgreSQL schema
├── scripts/
│   ├── generate_test_data.sh  # Test data generator
│   └── test_client.html       # WebSocket test client
├── Dockerfile           # Docker image definition
├── docker-compose.yml   # Docker Compose setup
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
└── README.md            # This file
```

## Development

To modify the whitelist, edit `configs/whitelist.txt` and either restart the service or POST to the `/whitelist` endpoint to reload it.

## Implementation Notes

1. **Bloom Filter**: The service uses a Bloom filter for efficient user address checking. It's initialized from Redis on startup and updated periodically.

2. **PostgreSQL Storage**: Events are stored in PostgreSQL with a 1-week retention policy. Old events are automatically cleaned up.

3. **File Position Tracking**: The service tracks its position in each file and saves this information to Redis for recovery after restarts.

4. **Event Types**: The service processes two types of events:
   - Fill events from `node_fills_by_block` directories
   - Miscellaneous events from `misc_events_by_block` directories