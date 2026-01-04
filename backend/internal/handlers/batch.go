package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type BatchHandler struct {
	DB *sqlx.DB
}

type BatchResponse struct {
	BatchID          string  `json:"batchId"`
	Status           string  `json:"status"`
	ProcessedCount   int     `json:"processedCount"`
	TotalTransactions *int    `json:"totalTransactions"`
	Counts           struct {
		AutoMatched  int `json:"autoMatched"`
		NeedsReview  int `json:"needsReview"`
		Unmatched    int `json:"unmatched"`
	} `json:"counts"`
	StartedAt   string  `json:"startedAt"`
	CompletedAt *string `json:"completedAt"`
	UpdatedAt   string  `json:"updatedAt"`
	ProgressPercent *float64 `json:"progressPercent,omitempty"`
}

func NewBatchHandler(db *sqlx.DB) *BatchHandler {
	return &BatchHandler{DB: db}
}

func (h *BatchHandler) GetBatch(c echo.Context) error {
	batchID := c.Param("batchId")

	// Query batch by PK only (fast, indexed)
	var batch struct {
		ID                string         `db:"id"`
		Status            string         `db:"status"`
		ProcessedCount    int            `db:"processed_count"`
		TotalTransactions sql.NullInt64  `db:"total_transactions"`
		AutoMatchedCount  int            `db:"auto_matched_count"`
		NeedsReviewCount  int            `db:"needs_review_count"`
		UnmatchedCount    int            `db:"unmatched_count"`
		StartedAt         time.Time      `db:"started_at"`
		CompletedAt       sql.NullTime   `db:"completed_at"`
		CreatedAt        time.Time      `db:"created_at"`
	}

	err := h.DB.Get(&batch, `
		SELECT 
			id::text,
			status::text,
			processed_count,
			total_transactions,
			auto_matched_count,
			needs_review_count,
			unmatched_count,
			started_at,
			completed_at,
			created_at
		FROM reconciliation_batches
		WHERE id = $1
	`, batchID)

	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "batch not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch batch"})
	}

	// Build response
	response := BatchResponse{
		BatchID:        batch.ID,
		Status:         batch.Status,
		ProcessedCount: batch.ProcessedCount,
		StartedAt:      batch.StartedAt.Format(time.RFC3339),
		UpdatedAt:      batch.CreatedAt.Format(time.RFC3339), // Using created_at as updated_at proxy
	}

	// Set total transactions (nullable)
	if batch.TotalTransactions.Valid {
		total := int(batch.TotalTransactions.Int64)
		response.TotalTransactions = &total
		
		// Calculate progress percent if total is known
		if total > 0 {
			percent := float64(batch.ProcessedCount) / float64(total) * 100.0
			if percent > 100.0 {
				percent = 100.0
			}
			response.ProgressPercent = &percent
		}
	}

	// Set counts
	response.Counts.AutoMatched = batch.AutoMatchedCount
	response.Counts.NeedsReview = batch.NeedsReviewCount
	response.Counts.Unmatched = batch.UnmatchedCount

	// Set completed at (nullable)
	if batch.CompletedAt.Valid {
		completedAt := batch.CompletedAt.Time.Format(time.RFC3339)
		response.CompletedAt = &completedAt
	}

	// Set cache control header to prevent caching
	c.Response().Header().Set("Cache-Control", "no-store")

	return c.JSON(http.StatusOK, response)
}
