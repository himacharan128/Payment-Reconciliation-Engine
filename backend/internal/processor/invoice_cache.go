package processor

import (
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type InvoiceCandidate struct {
	ID           string
	InvoiceNumber string
	Amount       string
	DueDate     time.Time
	CustomerName string
	NormalizedName string // Pre-normalized for matching
	Status       string
}

type InvoiceCache struct {
	ByAmount map[string][]*InvoiceCandidate // amount -> []InvoiceCandidate
	ByID     map[string]*InvoiceCandidate   // id -> InvoiceCandidate
}

func LoadInvoiceCache(db *sqlx.DB) (*InvoiceCache, error) {
	// Load eligible invoices: sent or overdue, not paid
	query := `
		SELECT 
			id::text,
			invoice_number,
			amount::text,
			due_date,
			customer_name,
			status
		FROM invoices
		WHERE status IN ('sent', 'overdue')
		AND (paid_at IS NULL OR status != 'paid')
	`

	var invoices []struct {
		ID           string    `db:"id"`
		InvoiceNumber string    `db:"invoice_number"`
		Amount       string    `db:"amount"`
		DueDate      time.Time `db:"due_date"`
		CustomerName string    `db:"customer_name"`
		Status       string    `db:"status"`
	}

	err := db.Select(&invoices, query)
	if err != nil {
		return nil, err
	}

	cache := &InvoiceCache{
		ByAmount: make(map[string][]*InvoiceCandidate),
		ByID:     make(map[string]*InvoiceCandidate),
	}

	for _, inv := range invoices {
		candidate := &InvoiceCandidate{
			ID:            inv.ID,
			InvoiceNumber: inv.InvoiceNumber,
			Amount:        inv.Amount,
			DueDate:      inv.DueDate,
			CustomerName: inv.CustomerName,
			NormalizedName: normalizeName(inv.CustomerName),
			Status:        inv.Status,
		}

		// Index by amount
		cache.ByAmount[inv.Amount] = append(cache.ByAmount[inv.Amount], candidate)
		cache.ByID[inv.ID] = candidate
	}

	return cache, nil
}

func normalizeName(name string) string {
	// Uppercase, remove punctuation, collapse spaces
	name = strings.ToUpper(name)
	name = strings.ReplaceAll(name, ",", " ")
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	
	// Collapse multiple spaces
	words := strings.Fields(name)
	return strings.Join(words, " ")
}

