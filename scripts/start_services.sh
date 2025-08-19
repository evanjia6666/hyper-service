#!/bin/bash

# Script to start services manually for testing

# Start Redis in background
echo "Starting Redis..."
redis-server --daemonize yes

# Start PostgreSQL in background
echo "Starting PostgreSQL..."
pg_ctl -D /usr/local/var/postgres -l /usr/local/var/postgres/server.log start

# Wait a bit for services to start
sleep 5

# Run the hyper-service
echo "Starting hyper-service..."
go run cmd/main.go -data_dir=test -port=8080