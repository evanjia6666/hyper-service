package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	config "github.com/uxuyprotocol/hyper-service/configs"
	"github.com/uxuyprotocol/hyper-service/internal/service"
)

func main() {
	dataDir := flag.String("data_dir", "", "Path to the data directory")
	port := flag.String("port", "8080", "Port for the WebSocket server")
	redisAddr := flag.String("redis_addr", "localhost:6379", "Redis server address")
	redisPassword := flag.String("redis_password", "", "Redis password")
	redisDB := flag.Int("redis_db", 0, "Redis database number")
	postgresAddr := flag.String("postgres_addr", "localhost:5432", "PostgreSQL server address")
	postgresUser := flag.String("postgres_user", "postgres", "PostgreSQL user")
	postgresPassword := flag.String("postgres_password", "postgres", "PostgreSQL password")
	postgresDB := flag.String("postgres_db", "hyper_service", "PostgreSQL database name")
	flag.Parse()

	if *dataDir == "" {
		log.Fatal("data_dir is required")
	}

	// Create the configuration
	cfg := &config.Config{
		DataDir:                *dataDir,
		Port:                   *port,
		RedisAddr:              *redisAddr,
		RedisPassword:          *redisPassword,
		RedisDB:                *redisDB,
		PostgresAddr:           *postgresAddr,
		PostgresUser:           *postgresUser,
		PostgresPassword:       *postgresPassword,
		PostgresDB:             *postgresDB,
		BloomExpectedItems:     100000000,
		BloomFalsePositiveRate: 0.01,
		FileCheckInterval:      5 * time.Second,
		CleanupInterval:        1 * time.Hour,
		EventRetention:         7 * 24 * time.Hour, // 1 week
	}

	// Create the service
	svc, err := service.NewService(cfg)
	if err != nil {
		log.Fatal("Failed to create service:", err)
	}

	// Start the service
	err = svc.Start()
	if err != nil {
		log.Fatal("Failed to start service:", err)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	svc.Stop()
	log.Println("Service stopped")
}
