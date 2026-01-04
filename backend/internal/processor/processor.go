package processor

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"payment-reconciliation-engine/backend/internal/worker"
)

type Processor struct {
	DB            *sqlx.DB
	Worker        *worker.Worker
	BatchID       string
	InvoiceCache  *InvoiceCache
	BatchSize     int
	ProgressEvery int
	MatchedInvoices map[string]bool // Track matched invoices to prevent duplicates
}

type TransactionRow struct {
	TransactionDate time.Time
	Description     string
	Amount          string
	ReferenceNumber *string
}

func ProcessJob(job *worker.Job, db *sqlx.DB, w *worker.Worker) error {
	startTime := time.Now()
	log.Printf("Starting CSV processing: batch_id=%s", job.BatchID)

	// Check if file content is available in database (preferred for Render multi-instance)
	if len(job.FileContent) == 0 {
		return fmt.Errorf("file content not found in database for batch %s", job.BatchID)
	}

	// Load invoice cache
	cacheStart := time.Now()
	cache, err := LoadInvoiceCache(db)
	if err != nil {
		return fmt.Errorf("failed to load invoice cache: %w", err)
	}
	log.Printf("Loaded %d invoices into cache (took %v)", len(cache.ByID), time.Since(cacheStart))

	// Create processor
	processor := &Processor{
		DB:            db,
		Worker:        w,
		BatchID:       job.BatchID,
		InvoiceCache:  cache,
		BatchSize:     500,
		ProgressEvery: 200,
		MatchedInvoices: make(map[string]bool),
	}

	// Process CSV from database content
	err = processor.processCSVFromContent(job.FileContent)
	if err != nil {
		return fmt.Errorf("CSV processing failed: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("CSV processing completed: batch_id=%s, duration=%v", job.BatchID, duration)
	return nil
}

func (p *Processor) processCSVFromContent(fileContent []byte) error {
	reader := csv.NewReader(strings.NewReader(string(fileContent)))
	
	// Read header
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Map column names to indices
	colMap := make(map[string]int)
	for i, col := range header {
		colMap[strings.ToLower(strings.TrimSpace(col))] = i
	}

	// Validate required columns
	required := []string{"transaction_date", "description", "amount"}
	for _, req := range required {
		if _, exists := colMap[req]; !exists {
			return fmt.Errorf("missing required column: %s", req)
		}
	}

	// Counters
	var processedCount, autoMatchedCount, needsReviewCount, unmatchedCount int
	var invalidRows int

	// Batch accumulator
	batch := make([]TransactionRow, 0, p.BatchSize)
	batchMatches := make([]MatchResult, 0, p.BatchSize)

	// Process rows
	rowNum := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading row %d: %v", rowNum+1, err)
			invalidRows++
			continue
		}

		rowNum++

		// Parse row
		row, parseErr := p.parseRow(record, colMap)
		if parseErr != nil {
			log.Printf("Invalid row %d: %v", rowNum, parseErr)
			invalidRows++
			continue
		}

		// Match transaction
		candidates := p.InvoiceCache.ByAmount[row.Amount]
		
		// Filter out already-matched invoices
		filteredCandidates := make([]*InvoiceCandidate, 0, len(candidates))
		for _, cand := range candidates {
			if !p.MatchedInvoices[cand.ID] {
				filteredCandidates = append(filteredCandidates, cand)
			}
		}
		
		match := MatchTransaction(row.Description, row.Amount, row.TransactionDate, filteredCandidates)
		
		// Mark invoice as matched if auto_matched or needs_review
		if match.InvoiceID != nil && (match.Status == "auto_matched" || match.Status == "needs_review") {
			p.MatchedInvoices[*match.InvoiceID] = true
		}

		// Accumulate for batch insert
		batch = append(batch, row)
		batchMatches = append(batchMatches, match)

		// Flush batch when full
		if len(batch) >= p.BatchSize {
			err := p.flushBatch(batch, batchMatches, &processedCount, &autoMatchedCount, &needsReviewCount, &unmatchedCount)
			if err != nil {
				return err
			}
			batch = batch[:0]
			batchMatches = batchMatches[:0]
		}
	}

	// Flush remaining rows
	if len(batch) > 0 {
		err := p.flushBatch(batch, batchMatches, &processedCount, &autoMatchedCount, &needsReviewCount, &unmatchedCount)
		if err != nil {
			return err
		}
	}

	// Finalize
	log.Printf("Processing complete: processed=%d, invalid=%d, auto_matched=%d, needs_review=%d, unmatched=%d",
		processedCount, invalidRows, autoMatchedCount, needsReviewCount, unmatchedCount)

	// Final update: set total transactions and final counts
	err = p.Worker.SetBatchTotal(p.BatchID, processedCount)
	if err != nil {
		return fmt.Errorf("failed to set total transactions: %w", err)
	}

	// Final count update to ensure accuracy
	err = p.Worker.UpdateBatchProgress(p.BatchID, processedCount, autoMatchedCount, needsReviewCount, unmatchedCount)
	if err != nil {
		log.Printf("Warning: Failed to update final batch counts: %v", err)
	}

	return nil
}

func (p *Processor) parseRow(record []string, colMap map[string]int) (TransactionRow, error) {
	var row TransactionRow

	// Parse date
	dateIdx, exists := colMap["transaction_date"]
	if !exists || dateIdx >= len(record) {
		return row, fmt.Errorf("missing transaction_date")
	}
	date, err := time.Parse("2006-01-02", record[dateIdx])
	if err != nil {
		return row, fmt.Errorf("invalid date format: %w", err)
	}
	row.TransactionDate = date

	// Parse description
	descIdx, exists := colMap["description"]
	if !exists || descIdx >= len(record) {
		return row, fmt.Errorf("missing description")
	}
	row.Description = record[descIdx]

	// Parse amount
	amountIdx, exists := colMap["amount"]
	if !exists || amountIdx >= len(record) {
		return row, fmt.Errorf("missing amount")
	}
	row.Amount = record[amountIdx]
	
	// Validate amount is numeric
	_, err = strconv.ParseFloat(row.Amount, 64)
	if err != nil {
		return row, fmt.Errorf("invalid amount: %w", err)
	}

	// Parse reference_number (optional)
	if refIdx, exists := colMap["reference_number"]; exists && refIdx < len(record) && record[refIdx] != "" {
		ref := record[refIdx]
		row.ReferenceNumber = &ref
	}

	return row, nil
}

func (p *Processor) flushBatch(
	rows []TransactionRow,
	matches []MatchResult,
	processedCount, autoMatchedCount, needsReviewCount, unmatchedCount *int,
) error {
	if len(rows) == 0 {
		return nil
	}

	startTime := time.Now()

	// Build insert query (multi-row insert)
	query := `
		INSERT INTO bank_transactions (
			upload_batch_id, transaction_date, description, amount, reference_number,
			status, matched_invoice_id, confidence_score, match_details
		) VALUES `
	
	args := make([]interface{}, 0, len(rows)*9)
	placeholders := make([]string, 0, len(rows))
	
	for i, row := range rows {
		match := matches[i]
		
		var invoiceID interface{}
		if match.InvoiceID != nil {
			invoiceID = *match.InvoiceID
		}
		
		var confidence interface{}
		if match.Status != "unmatched" {
			confidence = match.Confidence
		}
		
		// Convert match_details to JSONB-compatible format
		var matchDetailsJSON interface{}
		if match.MatchDetails != nil {
			// Marshal map to JSON bytes for PostgreSQL JSONB
			jsonBytes, err := json.Marshal(match.MatchDetails)
			if err != nil {
				log.Printf("Failed to marshal match_details: %v", err)
				matchDetailsJSON = "{}"
			} else {
				matchDetailsJSON = string(jsonBytes)
			}
		} else {
			matchDetailsJSON = "{}"
		}
		
		// Cast match_details to JSONB in SQL
		placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d::jsonb)",
			i*9+1, i*9+2, i*9+3, i*9+4, i*9+5, i*9+6, i*9+7, i*9+8, i*9+9))
		
		args = append(args,
			p.BatchID,
			row.TransactionDate,
			row.Description,
			row.Amount,
			row.ReferenceNumber,
			match.Status,
			invoiceID,
			confidence,
			matchDetailsJSON,
		)
	}

	fullQuery := query + strings.Join(placeholders, ", ")

	// Execute in transaction
	tx, err := p.DB.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(fullQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to insert batch: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Update counters after successful commit
	*processedCount += len(rows)
	for _, match := range matches {
		switch match.Status {
		case "auto_matched":
			*autoMatchedCount++
		case "needs_review":
			*needsReviewCount++
		case "unmatched":
			*unmatchedCount++
		}
	}

	// Update progress
	err = p.Worker.UpdateBatchProgress(p.BatchID, *processedCount, *autoMatchedCount, *needsReviewCount, *unmatchedCount)
	if err != nil {
		log.Printf("Warning: Failed to update progress: %v", err)
	}

	duration := time.Since(startTime)
	log.Printf("Flushed batch: %d rows in %v (%.0f rows/sec)", len(rows), duration, float64(len(rows))/duration.Seconds())

	return nil
}

