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
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id
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
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id
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

	previousInvoiceID := ""
	if current.MatchedInvoiceID.Valid {
		previousInvoiceID = current.MatchedInvoiceID.String
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
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id
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

	previousInvoiceID := ""
	if current.MatchedInvoiceID.Valid {
		previousInvoiceID = current.MatchedInvoiceID.String
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
	}
	err = tx.Get(&current, `
		SELECT status, matched_invoice_id
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

	previousInvoiceID := ""
	if current.MatchedInvoiceID.Valid {
		previousInvoiceID = current.MatchedInvoiceID.String
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

