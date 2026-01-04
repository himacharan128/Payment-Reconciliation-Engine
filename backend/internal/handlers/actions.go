package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type ActionsHandler struct {
	DB *sqlx.DB
}

type ConfirmRequest struct {
	Notes string `json:"notes"`
}

type ManualMatchRequest struct {
	InvoiceID string `json:"invoiceId"`
	Notes     string `json:"notes"`
}

type BulkConfirmRequest struct {
	BatchID string `json:"batchId"`
	Notes   string `json:"notes"`
}

func NewActionsHandler(db *sqlx.DB) *ActionsHandler {
	return &ActionsHandler{DB: db}
}

// updateBatchCounters updates batch counters when transaction status changes
// Uses direct query formatting to avoid prepared statement issues with Neon pooler
func (h *ActionsHandler) updateBatchCounters(tx *sqlx.Tx, batchID string, oldStatus, newStatus string) error {
	// Get current batch counters
	var batch struct {
		AutoMatchedCount  int `db:"auto_matched_count"`
		NeedsReviewCount  int `db:"needs_review_count"`
		UnmatchedCount    int `db:"unmatched_count"`
		ConfirmedCount   int `db:"confirmed_count"`
		ExternalCount     int `db:"external_count"`
	}
	err := tx.Get(&batch, `SELECT auto_matched_count, needs_review_count, unmatched_count, confirmed_count, external_count FROM reconciliation_batches WHERE id = $1`, batchID)
	if err != nil {
		return fmt.Errorf("failed to fetch batch counters: %w", err)
	}

	// Adjust counters based on status transition (decrease old status)
	if oldStatus == "auto_matched" {
		batch.AutoMatchedCount--
	} else if oldStatus == "needs_review" {
		batch.NeedsReviewCount--
	} else if oldStatus == "unmatched" {
		batch.UnmatchedCount--
	} else if oldStatus == "confirmed" {
		batch.ConfirmedCount--
	} else if oldStatus == "external" {
		batch.ExternalCount--
	}

	// Increase new status counter
	if newStatus == "auto_matched" {
		batch.AutoMatchedCount++
	} else if newStatus == "needs_review" {
		batch.NeedsReviewCount++
	} else if newStatus == "unmatched" {
		batch.UnmatchedCount++
	} else if newStatus == "confirmed" {
		batch.ConfirmedCount++
	} else if newStatus == "external" {
		batch.ExternalCount++
	}

	// Update batch counters (using direct query to avoid prepared statements)
	// Use Exec with formatted query string - safe because batchID is validated UUID and counts are integers
	query := fmt.Sprintf(`
		UPDATE reconciliation_batches
		SET auto_matched_count = %d,
		    needs_review_count = %d,
		    unmatched_count = %d,
		    confirmed_count = %d,
		    external_count = %d
		WHERE id = '%s'
	`, batch.AutoMatchedCount, batch.NeedsReviewCount, batch.UnmatchedCount, batch.ConfirmedCount, batch.ExternalCount, batchID)
	
	_, err = tx.Exec(query)
	return err
}

// ConfirmMatch confirms a suggested match
func (h *ActionsHandler) ConfirmMatch(c echo.Context) error {
	transactionID := c.Param("id")

	if _, err := uuid.Parse(transactionID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
	}

	var req ConfirmRequest
	if err := c.Bind(&req); err != nil {
		// Notes are optional, continue with empty notes
	}

	tx, err := h.DB.Beginx()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to begin transaction"})
	}
	defer tx.Rollback()

	// Lock and get current transaction state
	var current struct {
		Status          string         `db:"status"`
		MatchedInvoiceID sql.NullString `db:"matched_invoice_id"`
		BatchID         string         `db:"upload_batch_id"`
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id, upload_batch_id
		FROM bank_transactions
		WHERE id = $1
		FOR UPDATE
	`, transactionID)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "transaction not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch transaction"})
	}

	// Validate state transition
	if current.Status == "confirmed" {
		// Idempotent: already confirmed
		return c.JSON(http.StatusOK, map[string]string{"message": "already confirmed"})
	}
	if current.Status != "auto_matched" && current.Status != "needs_review" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("cannot confirm transaction with status %s", current.Status),
		})
	}
	if !current.MatchedInvoiceID.Valid {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "transaction has no matched invoice"})
	}

	// Update transaction
	_, err = tx.Exec(`
		UPDATE bank_transactions
		SET status = 'confirmed'
		WHERE id = $1
	`, transactionID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update transaction"})
	}

	// Update batch counters
	if err := h.updateBatchCounters(tx, current.BatchID, current.Status, "confirmed"); err != nil {
		log.Printf("Warning: Failed to update batch counters: %v", err)
		// Continue anyway - counters are eventually consistent
	}

	// Insert audit log
	_, err = tx.Exec(`
		INSERT INTO match_audit_logs (
			transaction_id, action, previous_invoice_id, new_invoice_id,
			performed_by, reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, transactionID, "confirmed", current.MatchedInvoiceID.String, current.MatchedInvoiceID.String,
		"system", req.Notes)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create audit log"})
	}

	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "match confirmed"})
}

// RejectMatch rejects a suggested match
func (h *ActionsHandler) RejectMatch(c echo.Context) error {
	transactionID := c.Param("id")

	if _, err := uuid.Parse(transactionID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
	}

	tx, err := h.DB.Beginx()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to begin transaction"})
	}
	defer tx.Rollback()

	// Lock and get current state
	var current struct {
		Status          string         `db:"status"`
		MatchedInvoiceID sql.NullString `db:"matched_invoice_id"`
		BatchID         string         `db:"upload_batch_id"`
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id, upload_batch_id
		FROM bank_transactions
		WHERE id = $1
		FOR UPDATE
	`, transactionID)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "transaction not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch transaction"})
	}

	// Validate state transition
	if current.Status != "auto_matched" && current.Status != "needs_review" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("cannot reject transaction with status %s", current.Status),
		})
	}

	var previousInvoiceID interface{}
	if current.MatchedInvoiceID.Valid {
		previousInvoiceID = current.MatchedInvoiceID.String
	} else {
		previousInvoiceID = nil
	}

	// Update transaction: clear match, set to unmatched
	_, err = tx.Exec(`
		UPDATE bank_transactions
		SET status = 'unmatched',
		    matched_invoice_id = NULL,
		    confidence_score = NULL
		WHERE id = $1
	`, transactionID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update transaction"})
	}

	// Update batch counters
	if err := h.updateBatchCounters(tx, current.BatchID, current.Status, "unmatched"); err != nil {
		log.Printf("Warning: Failed to update batch counters: %v", err)
		// Continue anyway - counters are eventually consistent
	}

	// Insert audit log
	_, err = tx.Exec(`
		INSERT INTO match_audit_logs (
			transaction_id, action, previous_invoice_id, new_invoice_id,
			performed_by, reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, transactionID, "rejected", previousInvoiceID, nil, "system", "Match rejected by admin")
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create audit log"})
	}

	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "match rejected"})
}

// ManualMatch manually assigns an invoice to a transaction
func (h *ActionsHandler) ManualMatch(c echo.Context) error {
	transactionID := c.Param("id")

	if _, err := uuid.Parse(transactionID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
	}

	var req ManualMatchRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if _, err := uuid.Parse(req.InvoiceID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid invoice id"})
	}

	tx, err := h.DB.Beginx()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to begin transaction"})
	}
	defer tx.Rollback()

	// Verify invoice exists and is eligible
	var invoice struct {
		ID     string `db:"id"`
		Status string `db:"status"`
		PaidAt sql.NullTime `db:"paid_at"`
	}
	err = tx.Get(&invoice, `
		SELECT id, status, paid_at
		FROM invoices
		WHERE id = $1
	`, req.InvoiceID)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "invoice not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch invoice"})
	}
	if invoice.Status == "paid" || invoice.PaidAt.Valid {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "cannot match to paid invoice"})
	}

	// Lock and get current transaction state
	var current struct {
		Status          string         `db:"status"`
		MatchedInvoiceID sql.NullString `db:"matched_invoice_id"`
		BatchID         string         `db:"upload_batch_id"`
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id, upload_batch_id
		FROM bank_transactions
		WHERE id = $1
		FOR UPDATE
	`, transactionID)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "transaction not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch transaction"})
	}

	var previousInvoiceID interface{}
	if current.MatchedInvoiceID.Valid {
		previousInvoiceID = current.MatchedInvoiceID.String
	} else {
		previousInvoiceID = nil
	}

	// Update transaction: set match and confirm
	_, err = tx.Exec(`
		UPDATE bank_transactions
		SET status = 'confirmed',
		    matched_invoice_id = $1,
		    confidence_score = 100.0
		WHERE id = $2
	`, req.InvoiceID, transactionID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update transaction"})
	}

	// Update batch counters
	if err := h.updateBatchCounters(tx, current.BatchID, current.Status, "confirmed"); err != nil {
		log.Printf("Warning: Failed to update batch counters: %v", err)
		// Continue anyway - counters are eventually consistent
	}

	// Insert audit log
	_, err = tx.Exec(`
		INSERT INTO match_audit_logs (
			transaction_id, action, previous_invoice_id, new_invoice_id,
			performed_by, reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, transactionID, "manual_matched", previousInvoiceID, req.InvoiceID, "system", req.Notes)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create audit log"})
	}

	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "invoice manually matched"})
}

// MarkExternal marks a transaction as external (no invoice in system)
func (h *ActionsHandler) MarkExternal(c echo.Context) error {
	transactionID := c.Param("id")

	if _, err := uuid.Parse(transactionID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
	}

	tx, err := h.DB.Beginx()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to begin transaction"})
	}
	defer tx.Rollback()

	// Lock and get current state
	var current struct {
		Status          string         `db:"status"`
		MatchedInvoiceID sql.NullString `db:"matched_invoice_id"`
		BatchID         string         `db:"upload_batch_id"`
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id, upload_batch_id
		FROM bank_transactions
		WHERE id = $1
		FOR UPDATE
	`, transactionID)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "transaction not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch transaction"})
	}

	if current.Status == "external" {
		// Idempotent
		return c.JSON(http.StatusOK, map[string]string{"message": "already marked as external"})
	}

	var previousInvoiceID interface{}
	if current.MatchedInvoiceID.Valid {
		previousInvoiceID = current.MatchedInvoiceID.String
	} else {
		previousInvoiceID = nil
	}

	// Update transaction
	_, err = tx.Exec(`
		UPDATE bank_transactions
		SET status = 'external',
		    matched_invoice_id = NULL
		WHERE id = $1
	`, transactionID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update transaction"})
	}

	// Update batch counters (adjust based on previous status)
	if err := h.updateBatchCounters(tx, current.BatchID, current.Status, "external"); err != nil {
		log.Printf("Warning: Failed to update batch counters: %v", err)
		// Continue anyway - counters are eventually consistent
	}

	// Insert audit log
	_, err = tx.Exec(`
		INSERT INTO match_audit_logs (
			transaction_id, action, previous_invoice_id, new_invoice_id,
			performed_by, reason, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, transactionID, "marked_external", previousInvoiceID, nil, "system", "Marked as external payment")
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create audit log"})
	}

	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "marked as external"})
}

// BulkConfirm confirms all auto_matched transactions in a batch
func (h *ActionsHandler) BulkConfirm(c echo.Context) error {
	var req BulkConfirmRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if _, err := uuid.Parse(req.BatchID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid batch id"})
	}

	startTime := time.Now()

	tx, err := h.DB.Beginx()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to begin transaction"})
	}
	defer tx.Rollback()

	// Use CTE for set-based update + audit log insert
	query := `
		WITH updated AS (
			UPDATE bank_transactions
			SET status = 'confirmed'
			WHERE upload_batch_id = $1 AND status = 'auto_matched'
			RETURNING id, matched_invoice_id
		)
		INSERT INTO match_audit_logs (
			transaction_id, action, previous_invoice_id, new_invoice_id,
			performed_by, reason, created_at
		)
		SELECT 
			updated.id,
			'confirmed',
			updated.matched_invoice_id,
			updated.matched_invoice_id,
			'system',
			$2,
			NOW()
		FROM updated
	`

	result, err := tx.Exec(query, req.BatchID, req.Notes)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to bulk confirm"})
	}

	rowsAffected, _ := result.RowsAffected()

	// Update batch counters: decrease auto_matched_count and increase confirmed_count (bulk operation)
	if rowsAffected > 0 {
		var batch struct {
			AutoMatchedCount int `db:"auto_matched_count"`
			ConfirmedCount   int `db:"confirmed_count"`
		}
		err = tx.Get(&batch, `SELECT auto_matched_count, confirmed_count FROM reconciliation_batches WHERE id = $1`, req.BatchID)
		if err == nil {
			newAutoMatched := batch.AutoMatchedCount - int(rowsAffected)
			if newAutoMatched < 0 {
				newAutoMatched = 0
			}
			newConfirmed := batch.ConfirmedCount + int(rowsAffected)
			// Use formatted query to avoid prepared statements (safe: validated UUID and integer)
			updateQuery := fmt.Sprintf(`UPDATE reconciliation_batches SET auto_matched_count = %d, confirmed_count = %d WHERE id = '%s'`, newAutoMatched, newConfirmed, req.BatchID)
			_, _ = tx.Exec(updateQuery) // Ignore error - counters are eventually consistent
		}
	}

	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
	}

	duration := time.Since(startTime)
	log.Printf("Bulk confirm: batch_id=%s, confirmed=%d, duration=%v", req.BatchID, rowsAffected, duration)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":      "bulk confirm completed",
		"confirmed":    rowsAffected,
		"duration":     duration.String(),
	})
}

