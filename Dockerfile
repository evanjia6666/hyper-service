# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o hyper-service cmd/main.go

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/hyper-service .

# Copy configs and scripts
COPY --from=builder /app/configs ./configs
COPY --from=builder /app/scripts ./scripts

# Expose port
EXPOSE 8000

# Command to run the executable
CMD ["./hyper-service", "-data_dir=/app/test", "-whitelist=/app/configs/whitelist.txt"]