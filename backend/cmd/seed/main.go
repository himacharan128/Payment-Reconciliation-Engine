package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

type Invoice struct {
	InvoiceNumber string     `db:"invoice_number"`
	CustomerName  string     `db:"customer_name"`
	CustomerEmail *string    `db:"customer_email"`
	Amount        string     `db:"amount"`
	Status        string     `db:"status"`
	DueDate       time.Time  `db:"due_date"`
	PaidAt        *time.Time `db:"paid_at"`
	CreatedAt     time.Time  `db:"created_at"`
}

func main() {
	var csvFile string
	flag.StringVar(&csvFile, "file", "", "Path to invoices CSV file (default: ../../seed/data/invoices.csv)")
	flag.Parse()

	if csvFile == "" {
		// Default to seed/data/invoices.csv relative to repo root
		// When running from backend/cmd/seed, go up 3 levels to repo root
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get working directory: %v", err)
		}
		// If running from backend/, go up one level. If from backend/cmd/seed, go up 3 levels
		repoRoot := wd
		if filepath.Base(wd) == "seed" {
			repoRoot = filepath.Dir(filepath.Dir(filepath.Dir(wd)))
		} else if filepath.Base(wd) == "cmd" {
			repoRoot = filepath.Dir(filepath.Dir(wd))
		} else if filepath.Base(wd) == "backend" {
			repoRoot = filepath.Dir(wd)
		}
		csvFile = filepath.Join(repoRoot, "seed", "data", "invoices.csv")
	}

	// Get DATABASE_URL from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	// Connect to database
	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Read CSV file
	file, err := os.Open(csvFile)
	if err != nil {
		log.Fatalf("Failed to open CSV file %s: %v", csvFile, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatalf("Failed to read CSV: %v", err)
	}

	if len(records) < 2 {
		log.Fatal("CSV file must have at least a header row and one data row")
	}

	// Parse records (skip header)
	invoices := make([]Invoice, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < 9 {
			log.Printf("Skipping row %d: insufficient columns", i+1)
			continue
		}

		// Parse dates
		dueDate, err := time.Parse("2006-01-02", record[6])
		if err != nil {
			log.Printf("Skipping row %d: invalid due_date: %v", i+1, err)
			continue
		}

		var paidAt *time.Time
		if record[7] != "" {
			paid, err := time.Parse(time.RFC3339, record[7])
			if err != nil {
				log.Printf("Skipping row %d: invalid paid_at: %v", i+1, err)
				continue
			}
			paidAt = &paid
		}

		createdAt, err := time.Parse(time.RFC3339, record[8])
		if err != nil {
			log.Printf("Skipping row %d: invalid created_at: %v", i+1, err)
			continue
		}

		var customerEmail *string
		if record[3] != "" {
			customerEmail = &record[3]
		}

		invoice := Invoice{
			InvoiceNumber: record[1],
			CustomerName:  record[2],
			CustomerEmail: customerEmail,
			Amount:        record[4],
			Status:        record[5],
			DueDate:       dueDate,
			PaidAt:        paidAt,
			CreatedAt:     createdAt,
		}

		invoices = append(invoices, invoice)
	}

	fmt.Printf("Parsed %d invoices from CSV\n", len(invoices))

	// Batch insert with ON CONFLICT - optimized for Neon
	startTime := time.Now()
	batchSize := 100 // Smaller batches for Neon pooler
	inserted := 0
	skipped := 0

	for i := 0; i < len(invoices); i += batchSize {
		end := i + batchSize
		if end > len(invoices) {
			end = len(invoices)
		}
		batch := invoices[i:end]

		// Start transaction
		tx, err := db.Beginx()
		if err != nil {
			log.Fatalf("Failed to begin transaction: %v", err)
		}

		// Build multi-row insert query with ON CONFLICT
		// Using VALUES clause with multiple rows for better performance
		query := `
			INSERT INTO invoices (invoice_number, customer_name, customer_email, amount, status, due_date, paid_at, created_at)
			VALUES `
		
		args := make([]interface{}, 0, len(batch)*8)
		placeholders := make([]string, 0, len(batch))
		
		for idx, inv := range batch {
			placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				idx*8+1, idx*8+2, idx*8+3, idx*8+4, idx*8+5, idx*8+6, idx*8+7, idx*8+8))
			args = append(args,
				inv.InvoiceNumber,
				inv.CustomerName,
				inv.CustomerEmail,
				inv.Amount,
				inv.Status,
				inv.DueDate,
				inv.PaidAt,
				inv.CreatedAt,
			)
		}
		
		fullQuery := query + strings.Join(placeholders, ", ") + `
			ON CONFLICT (invoice_number) DO NOTHING`

		// Execute batch insert
		result, err := tx.Exec(fullQuery, args...)
		if err != nil {
			tx.Rollback()
			log.Fatalf("Failed to insert batch: %v", err)
		}

		rowsAffected, _ := result.RowsAffected()
		inserted += int(rowsAffected)
		skipped += len(batch) - int(rowsAffected)

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Fatalf("Failed to commit transaction: %v", err)
		}
		
		if (i/batchSize+1)%5 == 0 {
			fmt.Printf("Processed %d/%d invoices...\n", end, len(invoices))
		}
	}

	duration := time.Since(startTime)

	// Get final count
	var totalCount int
	err = db.Get(&totalCount, "SELECT count(*) FROM invoices")
	if err != nil {
		log.Printf("Warning: Failed to get total count: %v", err)
	}

	// Report results
	fmt.Printf("\n=== Seeding Results ===\n")
	fmt.Printf("CSV rows parsed: %d\n", len(invoices))
	fmt.Printf("Inserted: %d\n", inserted)
	fmt.Printf("Skipped (duplicates): %d\n", skipped)
	fmt.Printf("Total invoices in DB: %d\n", totalCount)
	fmt.Printf("Time taken: %v\n", duration)
	fmt.Printf("\nSeed completed successfully!\n")
}
