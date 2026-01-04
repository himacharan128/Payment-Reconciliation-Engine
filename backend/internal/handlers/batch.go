package handlers

import (
	"database/sql"
	"errors"
	"fmt"
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
		AutoMatched  int     `json:"autoMatched"`
		NeedsReview  int     `json:"needsReview"`
		Unmatched    int     `json:"unmatched"`
		Confirmed    int     `json:"confirmed"`
		External     int     `json:"external"`
	} `json:"counts"`
	Totals           struct {
		AutoMatched  float64 `json:"autoMatched"`
		NeedsReview  float64 `json:"needsReview"`
		Unmatched    float64 `json:"unmatched"`
		Confirmed    float64 `json:"confirmed"`
		External     float64 `json:"external"`
	} `json:"totals"`
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
		ConfirmedCount   int            `db:"confirmed_count"`
		ExternalCount     int            `db:"external_count"`
		StartedAt         time.Time      `db:"started_at"`
		CompletedAt       sql.NullTime   `db:"completed_at"`
		CreatedAt        time.Time      `db:"created_at"`
	}

	// Validate batchID is a valid UUID format
	if len(batchID) != 36 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid batch id format"})
	}

	// Query batch - PostgreSQL will automatically convert string to UUID
	// Use COALESCE to handle missing columns gracefully (if migration hasn't run)
	err := h.DB.Get(&batch, `
		SELECT 
			id::text as id,
			status::text as status,
			processed_count,
			total_transactions,
			auto_matched_count,
			needs_review_count,
			unmatched_count,
			COALESCE(confirmed_count, 0) as confirmed_count,
			COALESCE(external_count, 0) as external_count,
			started_at,
			completed_at,
			created_at
		FROM reconciliation_batches
		WHERE id = $1
	`, batchID)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "batch not found"})
		}
		c.Logger().Errorf("Failed to fetch batch %s: %v", batchID, err)
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
	response.Counts.Confirmed = batch.ConfirmedCount
	response.Counts.External = batch.ExternalCount

	// Calculate dollar totals by status
	// Use direct query formatting for Neon pooler compatibility
	var totals struct {
		AutoMatched  sql.NullFloat64 `db:"auto_matched_total"`
		NeedsReview  sql.NullFloat64 `db:"needs_review_total"`
		Unmatched    sql.NullFloat64 `db:"unmatched_total"`
		Confirmed    sql.NullFloat64 `db:"confirmed_total"`
		External     sql.NullFloat64 `db:"external_total"`
	}
	
	// Use direct query with UUID validation to avoid prepared statement issues
	totalsQuery := fmt.Sprintf(`
		SELECT 
			COALESCE(SUM(CASE WHEN status = 'auto_matched' THEN amount ELSE 0 END), 0) as auto_matched_total,
			COALESCE(SUM(CASE WHEN status = 'needs_review' THEN amount ELSE 0 END), 0) as needs_review_total,
			COALESCE(SUM(CASE WHEN status = 'unmatched' THEN amount ELSE 0 END), 0) as unmatched_total,
			COALESCE(SUM(CASE WHEN status = 'confirmed' THEN amount ELSE 0 END), 0) as confirmed_total,
			COALESCE(SUM(CASE WHEN status = 'external' THEN amount ELSE 0 END), 0) as external_total
		FROM bank_transactions
		WHERE upload_batch_id = '%s'
	`, batchID)
	
	err = h.DB.Get(&totals, totalsQuery)
	
	if err != nil {
		c.Logger().Warnf("Failed to fetch dollar totals for batch %s: %v", batchID, err)
		// Continue with zero totals if query fails
		response.Totals.AutoMatched = 0
		response.Totals.NeedsReview = 0
		response.Totals.Unmatched = 0
		response.Totals.Confirmed = 0
		response.Totals.External = 0
	} else {
		// Handle NULL values (when no transactions exist yet)
		if totals.AutoMatched.Valid {
			response.Totals.AutoMatched = totals.AutoMatched.Float64
		} else {
			response.Totals.AutoMatched = 0
		}
		if totals.NeedsReview.Valid {
			response.Totals.NeedsReview = totals.NeedsReview.Float64
		} else {
			response.Totals.NeedsReview = 0
		}
		if totals.Unmatched.Valid {
			response.Totals.Unmatched = totals.Unmatched.Float64
		} else {
			response.Totals.Unmatched = 0
		}
		if totals.Confirmed.Valid {
			response.Totals.Confirmed = totals.Confirmed.Float64
		} else {
			response.Totals.Confirmed = 0
		}
		if totals.External.Valid {
			response.Totals.External = totals.External.Float64
		} else {
			response.Totals.External = 0
		}
	}

	// Set completed at (nullable)
	if batch.CompletedAt.Valid {
		completedAt := batch.CompletedAt.Time.Format(time.RFC3339)
		response.CompletedAt = &completedAt
	}

	// Set cache control header to prevent caching
	c.Response().Header().Set("Cache-Control", "no-store")

	return c.JSON(http.StatusOK, response)
}
