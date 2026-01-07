package processor

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Debug file for tracing - set via environment variable DEBUG_FILE
var debugFile *os.File

func InitDebugLog(filename string) {
	if filename == "" {
		return
	}
	var err error
	debugFile, err = os.Create(filename)
	if err != nil {
		log.Printf("Warning: Could not create debug file: %v", err)
		return
	}
	log.Printf("Debug logging enabled: %s", filename)
}

func CloseDebugLog() {
	if debugFile != nil {
		debugFile.Close()
		debugFile = nil
	}
}

func debugLog(format string, args ...interface{}) {
	if debugFile != nil {
		fmt.Fprintf(debugFile, format+"\n", args...)
	}
}

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
	log.Println("DEBUG: LoadInvoiceCache starting...")
	
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
		ORDER BY amount, due_date, id
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

	// Log first 5 invoice IDs from DB (before any sorting) to verify DB order
	var dbOrderHash string
	for i := 0; i < len(invoices) && i < 5; i++ {
		dbOrderHash += invoices[i].ID[:8] + ","
	}
	log.Printf("DEBUG: DB returned %d invoices, first 5 IDs: %s", len(invoices), dbOrderHash)

	// Sort invoices in Go for guaranteed determinism (don't rely on DB ORDER BY)
	sort.SliceStable(invoices, func(i, j int) bool {
		if invoices[i].Amount != invoices[j].Amount {
			return invoices[i].Amount < invoices[j].Amount
		}
		if !invoices[i].DueDate.Equal(invoices[j].DueDate) {
			return invoices[i].DueDate.Before(invoices[j].DueDate)
		}
		return invoices[i].ID < invoices[j].ID
	})

	// Log first 5 invoice IDs after Go sorting
	var goSortHash string
	for i := 0; i < len(invoices) && i < 5; i++ {
		goSortHash += invoices[i].ID[:8] + ","
	}
	log.Printf("DEBUG: After Go sort, first 5 IDs: %s", goSortHash)

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

	// Explicitly sort each amount's candidate list for deterministic ordering
	// This guarantees consistency regardless of database query order
	for _, candidates := range cache.ByAmount {
		sort.SliceStable(candidates, func(i, j int) bool {
			if !candidates[i].DueDate.Equal(candidates[j].DueDate) {
				return candidates[i].DueDate.Before(candidates[j].DueDate)
			}
			return candidates[i].ID < candidates[j].ID
		})
	}

	// Log a sample amount bucket to verify ordering (pick a common amount like 1100.00)
	if candidates, ok := cache.ByAmount["1100.00"]; ok && len(candidates) > 1 {
		var bucketHash string
		for i, c := range candidates {
			bucketHash += fmt.Sprintf("%d:%s,", i, c.ID[:8])
		}
		log.Printf("DEBUG: Amount=1100.00 has %d candidates: %s", len(candidates), bucketHash)
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

