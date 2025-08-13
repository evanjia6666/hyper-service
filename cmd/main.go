package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	
	"github.com/uxuyprotocol/hyper-service/internal/service"
)

func main() {
	dataDir := flag.String("data_dir", "", "Path to the data directory")
	whitelistFile := flag.String("whitelist", "configs/whitelist.txt", "Path to the whitelist file")
	port := flag.String("port", "8080", "Port for the WebSocket server")
	flag.Parse()

	if *dataDir == "" {
		log.Fatal("data_dir is required")
	}

	// Create the service
	svc, err := service.NewService(*dataDir, *whitelistFile, *port)
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