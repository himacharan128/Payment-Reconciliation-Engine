package handlers

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type TransactionsHandler struct {
	DB *sqlx.DB
}

type TransactionItem struct {
	ID              string   `json:"id"`
	TransactionDate string   `json:"transactionDate"`
	Amount          string   `json:"amount"`
	Description     string   `json:"description"`
	Status          string   `json:"status"`
	ConfidenceScore *float64 `json:"confidenceScore"`
	MatchedInvoiceID *string `json:"matchedInvoiceId"`
	ReferenceNumber *string `json:"referenceNumber,omitempty"`
}

type TransactionsResponse struct {
	Items      []TransactionItem `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

func NewTransactionsHandler(db *sqlx.DB) *TransactionsHandler {
	return &TransactionsHandler{DB: db}
}

func (h *TransactionsHandler) ListTransactions(c echo.Context) error {
	batchID := c.Param("batchId")
	
	// Validate batchId is a valid UUID
	if _, err := uuid.Parse(batchID); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid batchId format"})
	}

	// Parse query parameters
	status := c.QueryParam("status")
	limitStr := c.QueryParam("limit")
	cursor := c.QueryParam("cursor")

	// Validate and set limit
	limit := 50 // default
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil || parsedLimit < 1 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid limit"})
		}
		limit = parsedLimit
		if limit > 200 {
			limit = 200 // clamp to max
		}
	}

	// Validate status if provided
	validStatuses := map[string]bool{
		"auto_matched": true,
		"needs_review": true,
		"unmatched":    true,
		"confirmed":    true,
		"external":     true,
		"pending":      true,
	}
	if status != "" && status != "all" && !validStatuses[status] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid status filter"})
	}

	// Parse cursor if provided
	var cursorCreatedAt *time.Time
	var cursorID *string
	if cursor != "" {
		createdAt, id, err := decodeCursor(cursor)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
		}
		cursorCreatedAt = &createdAt
		cursorID = &id
	}

	// Build query based on whether status filter is applied
	var query string
	var args []interface{}
	argNum := 1
	
	if status == "" || status == "all" {
		// All tab - use (upload_batch_id, created_at, id) index
		query = `
			SELECT 
				id::text,
				transaction_date,
				amount::text,
				description,
				status::text,
				confidence_score,
				matched_invoice_id::text,
				reference_number,
				created_at
			FROM bank_transactions
			WHERE upload_batch_id = $` + strconv.Itoa(argNum)
		args = append(args, batchID)
		argNum++
		
		if cursorCreatedAt != nil {
			query += ` AND (created_at, id) < ($` + strconv.Itoa(argNum) + `, $` + strconv.Itoa(argNum+1) + `)`
			args = append(args, *cursorCreatedAt, *cursorID)
			argNum += 2
		}
		
		query += ` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(argNum)
		args = append(args, limit)
	} else {
		// Status filter - use (upload_batch_id, status, created_at, id) index
		query = `
			SELECT 
				id::text,
				transaction_date,
				amount::text,
				description,
				status::text,
				confidence_score,
				matched_invoice_id::text,
				reference_number,
				created_at
			FROM bank_transactions
			WHERE upload_batch_id = $` + strconv.Itoa(argNum) + ` AND status = $` + strconv.Itoa(argNum+1)
		args = append(args, batchID, status)
		argNum += 2
		
		if cursorCreatedAt != nil {
			query += ` AND (created_at, id) < ($` + strconv.Itoa(argNum) + `, $` + strconv.Itoa(argNum+1) + `)`
			args = append(args, *cursorCreatedAt, *cursorID)
			argNum += 2
		}
		
		query += ` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(argNum)
		args = append(args, limit)
	}

	// Execute query
	type dbRow struct {
		ID              string          `db:"id"`
		TransactionDate time.Time       `db:"transaction_date"`
		Amount          string          `db:"amount"`
		Description     string          `db:"description"`
		Status          string          `db:"status"`
		ConfidenceScore sql.NullFloat64 `db:"confidence_score"`
		MatchedInvoiceID sql.NullString `db:"matched_invoice_id"`
		ReferenceNumber sql.NullString `db:"reference_number"`
		CreatedAt       time.Time       `db:"created_at"`
	}

	var rows []dbRow
	err := h.DB.Select(&rows, query, args...)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch transactions"})
	}

	// Convert to response items
	items := make([]TransactionItem, 0, len(rows))
	for _, row := range rows {
		// Truncate description to 120 chars for list view
		description := row.Description
		if len(description) > 120 {
			description = description[:120] + "..."
		}

		item := TransactionItem{
			ID:              row.ID,
			TransactionDate: row.TransactionDate.Format("2006-01-02"),
			Amount:          row.Amount,
			Description:     description,
			Status:          row.Status,
		}

		if row.ConfidenceScore.Valid {
			item.ConfidenceScore = &row.ConfidenceScore.Float64
		}

		if row.MatchedInvoiceID.Valid {
			item.MatchedInvoiceID = &row.MatchedInvoiceID.String
		}

		if row.ReferenceNumber.Valid {
			item.ReferenceNumber = &row.ReferenceNumber.String
		}

		items = append(items, item)
	}

	// Determine next cursor
	var nextCursor *string
	if len(items) == limit && len(items) > 0 {
		lastItem := rows[len(rows)-1]
		encoded := encodeCursor(lastItem.CreatedAt, lastItem.ID)
		nextCursor = &encoded
	}

	response := TransactionsResponse{
		Items:      items,
		NextCursor: nextCursor,
	}

	// Set cache control
	c.Response().Header().Set("Cache-Control", "no-store")

	return c.JSON(http.StatusOK, response)
}

func encodeCursor(createdAt time.Time, id string) string {
	// Format: <createdAtRFC3339Nano>|<uuid>
	raw := fmt.Sprintf("%s|%s", createdAt.Format(time.RFC3339Nano), id)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	decoded, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid base64: %w", err)
	}

	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid timestamp: %w", err)
	}

	id := parts[1]
	if _, err := uuid.Parse(id); err != nil {
		return time.Time{}, "", fmt.Errorf("invalid uuid: %w", err)
	}

	return createdAt, id, nil
}

