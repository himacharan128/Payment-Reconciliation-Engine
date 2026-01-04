package handlers

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/jmoiron/sqlx"
)

type UploadHandler struct {
	DB        *sqlx.DB
	UploadDir string
	MaxSize   int64 // Max file size in bytes (50MB default)
}

type UploadResponse struct {
	BatchID string `json:"batchId"`
	Status  string `json:"status"`
}

func NewUploadHandler(db *sqlx.DB, uploadDir string) *UploadHandler {
	return &UploadHandler{
		DB:        db,
		UploadDir: uploadDir,
		MaxSize:   50 * 1024 * 1024, // 50MB
	}
}

func (h *UploadHandler) Upload(c echo.Context) error {
	// Get file from form
	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no file provided"})
	}

	// Validate filename
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".csv") {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file must be a CSV"})
	}

	// Validate file size
	if file.Size > h.MaxSize {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("file exceeds maximum size of %d bytes", h.MaxSize)})
	}

	// Open uploaded file for header validation
	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open uploaded file"})
	}

	// Validate CSV header (read first line)
	reader := csv.NewReader(src)
	header, err := reader.Read()
	src.Close() // Close after reading header
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid CSV: cannot read header"})
	}

	// Validate required columns
	requiredCols := map[string]bool{
		"id":                false,
		"transaction_date":  false,
		"description":       false,
		"amount":            false,
		"reference_number": false,
	}
	for _, col := range header {
		colLower := strings.ToLower(strings.TrimSpace(col))
		if _, exists := requiredCols[colLower]; exists {
			requiredCols[colLower] = true
		}
	}

	missingCols := []string{}
	for col, found := range requiredCols {
		if !found {
			missingCols = append(missingCols, col)
		}
	}
	if len(missingCols) > 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("missing required columns: %s", strings.Join(missingCols, ", ")),
		})
	}

	// Reopen file for streaming write
	src, err = file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to reopen file"})
	}
	defer src.Close()

	// Generate batch ID
	batchID := uuid.New().String()

	// Ensure upload directory exists
	if err := os.MkdirAll(h.UploadDir, 0755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create upload directory"})
	}

	// Stream write file to disk
	filePath := filepath.Join(h.UploadDir, batchID+".csv")
	dst, err := os.Create(filePath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create file"})
	}

	bytesWritten, err := io.Copy(dst, src)
	if err != nil {
		dst.Close()
		os.Remove(filePath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write file"})
	}
	dst.Close()

	if bytesWritten == 0 {
		os.Remove(filePath)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file is empty"})
	}

	// Create batch and job in transaction
	tx, err := h.DB.Beginx()
	if err != nil {
		os.Remove(filePath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to begin transaction"})
	}

	// Insert batch
	_, err = tx.Exec(`
		INSERT INTO reconciliation_batches (id, filename, status, processed_count, auto_matched_count, needs_review_count, unmatched_count, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`, batchID, file.Filename, "processing", 0, 0, 0, 0)
	if err != nil {
		tx.Rollback()
		os.Remove(filePath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create batch"})
	}

	// Insert job
	_, err = tx.Exec(`
		INSERT INTO reconciliation_jobs (batch_id, file_path, status, attempts)
		VALUES ($1, $2, $3, $4)
	`, batchID, filePath, "queued", 0)
	if err != nil {
		tx.Rollback()
		os.Remove(filePath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create job"})
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		os.Remove(filePath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
	}

	return c.JSON(http.StatusCreated, UploadResponse{
		BatchID: batchID,
		Status:  "processing",
	})
}

