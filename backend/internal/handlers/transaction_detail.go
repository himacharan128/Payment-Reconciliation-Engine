package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type TransactionDetailHandler struct {
	DB *sqlx.DB
}

type TransactionDetailResponse struct {
	ID                string                 `json:"id"`
	UploadBatchID     string                 `json:"uploadBatchId"`
	TransactionDate   string                 `json:"transactionDate"`
	Amount            string                 `json:"amount"`
	Description       string                 `json:"description"`
	ReferenceNumber   *string                `json:"referenceNumber"`
	Status            string                 `json:"status"`
	ConfidenceScore   *float64               `json:"confidenceScore"`
	MatchedInvoiceID  *string                `json:"matchedInvoiceId"`
	MatchDetails      map[string]interface{} `json:"matchDetails"`
	CreatedAt         string                 `json:"createdAt"`
	UpdatedAt         string                 `json:"updatedAt"`
	Invoice           *InvoiceDetail         `json:"invoice,omitempty"`
	CanConfirm        bool                   `json:"canConfirm"`
	CanReject         bool                   `json:"canReject"`
	CanManualMatch    bool                   `json:"canManualMatch"`
	CanMarkExternal   bool                   `json:"canMarkExternal"`
}

type InvoiceDetail struct {
	ID            string  `json:"id"`
	InvoiceNumber string  `json:"invoiceNumber"`
	CustomerName  string  `json:"customerName"`
	CustomerEmail *string `json:"customerEmail"`
	Amount        string  `json:"amount"`
	DueDate       string  `json:"dueDate"`
	Status        string  `json:"status"`
}

func NewTransactionDetailHandler(db *sqlx.DB) *TransactionDetailHandler {
	return &TransactionDetailHandler{DB: db}
}

func (h *TransactionDetailHandler) GetTransaction(c echo.Context) error {
	transactionID := c.Param("id")

	// Validate transactionID is a valid UUID
	if _, err := uuid.Parse(transactionID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid transaction id format"})
	}

	// Query transaction with optional invoice join
	type dbRow struct {
		// Transaction fields
		ID                string          `db:"id"`
		UploadBatchID     string          `db:"upload_batch_id"`
		TransactionDate   time.Time       `db:"transaction_date"`
		Amount            string          `db:"amount"`
		Description       string          `db:"description"`
		ReferenceNumber   sql.NullString  `db:"reference_number"`
		Status            string          `db:"status"`
		ConfidenceScore   sql.NullFloat64 `db:"confidence_score"`
		MatchedInvoiceID  sql.NullString  `db:"matched_invoice_id"`
		MatchDetails      sql.NullString  `db:"match_details"` // JSONB as string
		CreatedAt         time.Time       `db:"created_at"`
		
		// Invoice fields (nullable)
		InvoiceID         sql.NullString `db:"invoice_id"`
		InvoiceNumber     sql.NullString `db:"invoice_number"`
		InvoiceCustomerName sql.NullString `db:"invoice_customer_name"`
		InvoiceCustomerEmail sql.NullString `db:"invoice_customer_email"`
		InvoiceAmount     sql.NullString `db:"invoice_amount"`
		InvoiceDueDate    sql.NullTime   `db:"invoice_due_date"`
		InvoiceStatus     sql.NullString `db:"invoice_status"`
	}

	query := `
		SELECT 
			bt.id::text,
			bt.upload_batch_id::text,
			bt.transaction_date,
			bt.amount::text,
			bt.description,
			bt.reference_number,
			bt.status::text,
			bt.confidence_score,
			bt.matched_invoice_id::text,
			bt.match_details::text,
			bt.created_at,
			i.id::text AS invoice_id,
			i.invoice_number AS invoice_number,
			i.customer_name AS invoice_customer_name,
			i.customer_email AS invoice_customer_email,
			i.amount::text AS invoice_amount,
			i.due_date AS invoice_due_date,
			i.status::text AS invoice_status
		FROM bank_transactions bt
		LEFT JOIN invoices i ON bt.matched_invoice_id = i.id
		WHERE bt.id = $1
	`

	var row dbRow
	err := h.DB.Get(&row, query, transactionID)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "transaction not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch transaction"})
	}

	// Build response
	response := TransactionDetailResponse{
		ID:              row.ID,
		UploadBatchID:   row.UploadBatchID,
		TransactionDate: row.TransactionDate.Format("2006-01-02"),
		Amount:          row.Amount,
		Description:     row.Description,
		Status:          row.Status,
		CreatedAt:       row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       row.CreatedAt.Format(time.RFC3339), // Using created_at as proxy
	}

	if row.ReferenceNumber.Valid {
		response.ReferenceNumber = &row.ReferenceNumber.String
	}

	if row.ConfidenceScore.Valid {
		response.ConfidenceScore = &row.ConfidenceScore.Float64
	}

	if row.MatchedInvoiceID.Valid {
		response.MatchedInvoiceID = &row.MatchedInvoiceID.String
	}

	// Parse match_details JSONB
	if row.MatchDetails.Valid && row.MatchDetails.String != "" {
		var matchDetails map[string]interface{}
		if err := json.Unmarshal([]byte(row.MatchDetails.String), &matchDetails); err == nil {
			response.MatchDetails = matchDetails
		} else {
			// If parsing fails, return empty map
			response.MatchDetails = map[string]interface{}{}
		}
	} else {
		response.MatchDetails = map[string]interface{}{}
	}

	// Include matched invoice if present
	if row.InvoiceID.Valid {
		invoice := &InvoiceDetail{
			ID:            row.InvoiceID.String,
			InvoiceNumber: row.InvoiceNumber.String,
			CustomerName:  row.InvoiceCustomerName.String,
			Amount:        row.InvoiceAmount.String,
			DueDate:       row.InvoiceDueDate.Time.Format("2006-01-02"),
			Status:        row.InvoiceStatus.String,
		}

		if row.InvoiceCustomerEmail.Valid {
			invoice.CustomerEmail = &row.InvoiceCustomerEmail.String
		}

		response.Invoice = invoice
	}

	// Determine actionability flags based on status
	response.CanConfirm = row.Status == "needs_review" || row.Status == "auto_matched"
	response.CanReject = row.Status == "needs_review" || row.Status == "auto_matched"
	response.CanManualMatch = row.Status == "unmatched" || row.Status == "needs_review"
	response.CanMarkExternal = row.Status == "unmatched"

	// Set cache control
	c.Response().Header().Set("Cache-Control", "no-store")

	return c.JSON(http.StatusOK, response)
}

