package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"payment-reconciliation-engine/backend/internal/db"
	"payment-reconciliation-engine/backend/internal/handlers"
)

func main() {
	// Connect to database
	database, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Get upload directory
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		// Default to /tmp/uploads for production, ./data/uploads for local
		if os.Getenv("APP_ENV") == "production" || os.Getenv("RENDER") == "true" {
			uploadDir = "/tmp/uploads"
		} else {
			uploadDir = "./data/uploads"
		}
	}

	// Ensure upload directory exists
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}

	// Initialize Echo
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// CORS middleware - allow all origins (no restrictions)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOriginFunc: func(origin string) (bool, error) {
			return true, nil // Allow all origins - no restrictions
		},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
		AllowCredentials: true,
	}))

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Handlers
	uploadHandler := handlers.NewUploadHandler(database, uploadDir)
	batchHandler := handlers.NewBatchHandler(database)
	transactionsHandler := handlers.NewTransactionsHandler(database)
	invoicesHandler := handlers.NewInvoicesHandler(database)
	transactionDetailHandler := handlers.NewTransactionDetailHandler(database)
	actionsHandler := handlers.NewActionsHandler(database)

	// Routes
	e.POST("/api/reconciliation/upload", uploadHandler.Upload)
	e.GET("/api/reconciliation/:batchId", batchHandler.GetBatch)
	e.GET("/api/reconciliation/:batchId/transactions", transactionsHandler.ListTransactions)
	
	// Debug: List all routes
	e.GET("/debug/routes", func(c echo.Context) error {
		routes := []string{}
		for _, route := range e.Routes() {
			routes = append(routes, route.Method+" "+route.Path)
		}
		return c.JSON(http.StatusOK, map[string]interface{}{"routes": routes})
	})
	e.GET("/api/invoices/search", invoicesHandler.SearchInvoices)
	e.GET("/api/transactions/:id", transactionDetailHandler.GetTransaction)
	
	// Action endpoints
	e.POST("/api/transactions/:id/confirm", actionsHandler.ConfirmMatch)
	e.POST("/api/transactions/:id/reject", actionsHandler.RejectMatch)
	e.POST("/api/transactions/:id/match", actionsHandler.ManualMatch)
	e.POST("/api/transactions/:id/external", actionsHandler.MarkExternal)
	e.POST("/api/transactions/bulk-confirm", actionsHandler.BulkConfirm)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	go func() {
		if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Printf("API server started on port %s", port)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
	log.Println("Shutting down...")
}
