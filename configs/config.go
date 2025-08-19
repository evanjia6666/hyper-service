package config

import (
	"time"
)

// Config holds the service configuration
type Config struct {
	// Data directory containing the event files
	DataDir string

	// WebSocket server port
	Port string

	// Redis configuration
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// PostgreSQL configuration
	PostgresAddr     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string

	// Bloom filter settings
	BloomExpectedItems     uint
	BloomFalsePositiveRate float64

	// File watching settings
	FileCheckInterval time.Duration

	// Database cleanup settings
	CleanupInterval time.Duration
	EventRetention  time.Duration
}
