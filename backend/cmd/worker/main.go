package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"payment-reconciliation-engine/backend/internal/db"
	"payment-reconciliation-engine/backend/internal/processor"
	"payment-reconciliation-engine/backend/internal/worker"
)

func main() {
	log.Println("Worker starting...")

	// Connect to database
	database, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Create worker
	w := worker.NewWorker(database)

	// Set CSV processing function
	w.ProcessJobFunc = func(job *worker.Job) error {
		return processor.ProcessJob(job, database, w)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start worker in goroutine
	go w.Start()

	// Wait for interrupt
	<-sigChan
	log.Println("Shutting down worker...")
}
