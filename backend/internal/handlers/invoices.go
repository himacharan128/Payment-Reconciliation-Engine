package handlers

import (
	"database/sql"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type InvoicesHandler struct {
	DB *sqlx.DB
}

type InvoiceSummary struct {
	ID            string  `json:"id"`
	InvoiceNumber string  `json:"invoiceNumber"`
	CustomerName  string  `json:"customerName"`
	CustomerEmail *string `json:"customerEmail"`
	Amount        string  `json:"amount"`
	DueDate       string  `json:"dueDate"`
	Status        string  `json:"status"`
}

type InvoicesSearchResponse struct {
	Items []InvoiceSummary `json:"items"`
}

func NewInvoicesHandler(db *sqlx.DB) *InvoicesHandler {
	return &InvoicesHandler{DB: db}
}

func (h *InvoicesHandler) SearchInvoices(c echo.Context) error {
	startTime := time.Now()

	// Parse query parameters
	q := strings.TrimSpace(c.QueryParam("q"))
	amountStr := strings.TrimSpace(c.QueryParam("amount"))
	status := strings.TrimSpace(c.QueryParam("status"))
	fromDateStr := strings.TrimSpace(c.QueryParam("fromDate"))
	toDateStr := strings.TrimSpace(c.QueryParam("toDate"))
	limitStr := c.QueryParam("limit")

	// Validate: require at least one filter
	if q == "" && amountStr == "" && status == "" && fromDateStr == "" && toDateStr == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "at least one filter required (q, amount, status, fromDate, or toDate)",
		})
	}

	// Validate and set limit
	limit := 20 // default
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil || parsedLimit < 1 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid limit"})
		}
		limit = parsedLimit
		if limit > 50 {
			limit = 50 // clamp to max
		}
	}

	// Validate q length if provided
	if q != "" && len(q) < 2 {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "search query must be at least 2 characters",
		})
	}

	// Validate status if provided
	validStatuses := map[string]bool{
		"draft":   true,
		"sent":    true,
		"paid":    true,
		"overdue": true,
	}
	if status != "" && !validStatuses[status] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid status"})
	}

	// Parse amount if provided
	var amount *float64
	if amountStr != "" {
		parsedAmount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid amount format"})
		}
		amount = &parsedAmount
	}

	// Parse date range if provided
	var fromDate, toDate *time.Time
	if fromDateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", fromDateStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid fromDate format (expected YYYY-MM-DD)"})
		}
		fromDate = &parsedDate
	}
	if toDateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", toDateStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid toDate format (expected YYYY-MM-DD)"})
		}
		toDate = &parsedDate
	}

	// Validate date range
	if fromDate != nil && toDate != nil && fromDate.After(*toDate) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "fromDate must be before or equal to toDate"})
	}

	// Build query
	query, args := h.buildSearchQuery(q, amount, status, fromDate, toDate, limit)
	
	// Execute query
	type dbRow struct {
		ID            string         `db:"id"`
		InvoiceNumber string         `db:"invoice_number"`
		CustomerName  string         `db:"customer_name"`
		CustomerEmail sql.NullString `db:"customer_email"`
		Amount        string         `db:"amount"`
		DueDate       time.Time       `db:"due_date"`
		Status        string         `db:"status"`
	}

	var rows []dbRow
	err := h.DB.Select(&rows, query, args...)
	if err != nil {
		log.Printf("Error searching invoices: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to search invoices"})
	}

	// Convert to response items
	items := make([]InvoiceSummary, 0, len(rows))
	for _, row := range rows {
		item := InvoiceSummary{
			ID:            row.ID,
			InvoiceNumber: row.InvoiceNumber,
			CustomerName:  row.CustomerName,
			Amount:        row.Amount,
			DueDate:       row.DueDate.Format("2006-01-02"),
			Status:        row.Status,
		}

		if row.CustomerEmail.Valid {
			item.CustomerEmail = &row.CustomerEmail.String
		}

		items = append(items, item)
	}

	duration := time.Since(startTime)
	log.Printf("Invoice search: q=%q, amount=%v, status=%q, fromDate=%v, toDate=%v, limit=%d, results=%d, duration=%v",
		q, amount, status, fromDate, toDate, limit, len(items), duration)

	response := InvoicesSearchResponse{
		Items: items,
	}

	// Set cache control
	c.Response().Header().Set("Cache-Control", "no-store")

	return c.JSON(http.StatusOK, response)
}

func (h *InvoicesHandler) buildSearchQuery(q string, amount *float64, status string, fromDate, toDate *time.Time, limit int) (string, []interface{}) {
	query := `
		SELECT 
			id::text,
			invoice_number,
			customer_name,
			customer_email,
			amount::text,
			due_date,
			status::text
		FROM invoices
		WHERE 1=1
	`
	
	args := []interface{}{}
	argNum := 1

	// Phase 1: Exact filters (btree indexes)
	if amount != nil {
		query += ` AND amount = $` + strconv.Itoa(argNum)
		args = append(args, *amount)
		argNum++
	}

	if status != "" {
		query += ` AND status = $` + strconv.Itoa(argNum)
		args = append(args, status)
		argNum++
	}

	// Date range filters
	if fromDate != nil {
		query += ` AND due_date >= $` + strconv.Itoa(argNum)
		args = append(args, *fromDate)
		argNum++
	}

	if toDate != nil {
		query += ` AND due_date <= $` + strconv.Itoa(argNum)
		args = append(args, *toDate)
		argNum++
	}

	// Phase 2: Text search (trigram indexes)
	if q != "" {
		// Check if q looks like an invoice number (contains digits/hyphens)
		isInvoiceNumber := regexp.MustCompile(`[\d-]`).MatchString(q)
		
		if isInvoiceNumber {
			// Search invoice_number first (exact or partial)
			query += ` AND invoice_number ILIKE $` + strconv.Itoa(argNum)
			args = append(args, "%"+q+"%")
			argNum++
		} else {
			// Fuzzy search on customer_name using trigram similarity
			// Use ILIKE with trigram index for fast partial matching
			query += ` AND customer_name ILIKE $` + strconv.Itoa(argNum)
			args = append(args, "%"+q+"%")
			argNum++
		}
	}

	// Ordering: prioritize exact matches, then by due_date ascending
	if amount != nil {
		// If amount filter is present, results are already filtered by exact amount
		query += ` ORDER BY due_date ASC`
	} else if q != "" {
		// If text search, order by similarity (trigram index helps here)
		// For simplicity, order by due_date ASC (useful for matching)
		query += ` ORDER BY due_date ASC`
	} else {
		// Default: order by due_date ASC
		query += ` ORDER BY due_date ASC`
	}

	// Limit
	query += ` LIMIT $` + strconv.Itoa(argNum)
	args = append(args, limit)

	return query, args
}

