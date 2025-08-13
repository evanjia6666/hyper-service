# Hyper Service

A Go service that monitors hypervisor node data files and provides real-time WebSocket updates for whitelisted users.

## Features

- Real-time monitoring of log-style data files
- WebSocket server with user-specific subscription support
- Whitelist-based filtering of events
- Automatic hourly file switching

## Prerequisites

- Go 1.21 or later
- Bash shell for running test scripts

## Installation

```bash
go mod tidy
```

## Usage

### Running the Service

```bash
go run cmd/main.go -data_dir=/path/to/data -whitelist=configs/whitelist.txt -port=8080
```

Parameters:
- `data_dir`: Path to the directory containing node data files
- `whitelist`: Path to the whitelist file (default: configs/whitelist.txt)
- `port`: Port for the WebSocket server (default: 8080)

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
    "trade": "trade"
  }
  ```

- Unsubscribe from events:
  ```json
  {
    "action": "unsubscribe",
    "trade": "trade"
  }
  ```

### HTTP Endpoints

- Whitelist management: `http://localhost:8080/whitelist` (GET to retrieve, POST to reload)

## Project Structure

```
.
├── cmd/
│   └── main.go          # Main application entry point
├── configs/
│   └── whitelist.txt    # Whitelisted user addresses
├── internal/
│   └── service/
│       └── service.go   # Core service implementation
├── scripts/
│   ├── generate_test_data.sh  # Test data generator
│   └── test_client.html       # WebSocket test client
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
└── README.md            # This file
```

## Development

To modify the whitelist, edit `configs/whitelist.txt` and either restart the service or POST to the `/whitelist` endpoint to reload it.