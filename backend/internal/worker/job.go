package worker

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type Job struct {
	ID        string    `db:"id"`
	BatchID   string    `db:"batch_id"`
	FilePath  string    `db:"file_path"`
	Status    string    `db:"status"`
	Attempts  int       `db:"attempts"`
	LastError *string   `db:"last_error"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Worker struct {
	DB                *sqlx.DB
	PollInterval      time.Duration
	StaleThreshold    time.Duration
	MaxAttempts       int
	ProgressEvery     int
	ProcessJobFunc    func(*Job) error // Will be set in Step 8
}

func NewWorker(db *sqlx.DB) *Worker {
	pollIntervalMs := 1000 // default
	if ms := os.Getenv("JOB_POLL_INTERVAL_MS"); ms != "" {
		if parsed, err := strconv.Atoi(ms); err == nil {
			pollIntervalMs = parsed
		}
	}

	progressEvery := 200 // default
	if pe := os.Getenv("BATCH_PROGRESS_UPDATE_EVERY"); pe != "" {
		if parsed, err := strconv.Atoi(pe); err == nil {
			progressEvery = parsed
		}
	}

	return &Worker{
		DB:             db,
		PollInterval:   time.Duration(pollIntervalMs) * time.Millisecond,
		StaleThreshold: 10 * time.Minute,
		MaxAttempts:    1, // Simple: no retries for interview
		ProgressEvery:  progressEvery,
	}
}

func (w *Worker) Start() {
	log.Println("Worker started")
	log.Printf("Poll interval: %v", w.PollInterval)
	log.Printf("Stale threshold: %v", w.StaleThreshold)
	log.Printf("Max attempts: %d", w.MaxAttempts)

	// Recover stale jobs on startup
	w.recoverStaleJobs()

	// Main polling loop
	for {
		job, err := w.claimJob()
		if err != nil {
			log.Printf("Error claiming job: %v", err)
			time.Sleep(w.PollInterval)
			continue
		}

		if job == nil {
			// No jobs available, sleep
			time.Sleep(w.PollInterval)
			continue
		}

		// Process the job
		w.processJob(job)
	}
}

func (w *Worker) recoverStaleJobs() {
	query := `
		UPDATE reconciliation_jobs 
		SET status = 'queued', updated_at = NOW()
		WHERE status = 'processing' 
		AND updated_at < NOW() - $1::interval
	`
	result, err := w.DB.Exec(query, fmt.Sprintf("%d minutes", int(w.StaleThreshold.Minutes())))
	if err != nil {
		log.Printf("Warning: Failed to recover stale jobs: %v", err)
		return
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Recovered %d stale job(s)", rowsAffected)
	}
}

func (w *Worker) claimJob() (*Job, error) {
	tx, err := w.DB.Beginx()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Find and lock a queued job (or stale processing job)
	query := `
		SELECT id, batch_id, file_path, status, attempts, last_error, created_at, updated_at
		FROM reconciliation_jobs
		WHERE status = 'queued'
		   OR (status = 'processing' AND updated_at < NOW() - $1::interval)
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`

	var job Job
	err = tx.Get(&job, query, fmt.Sprintf("%d minutes", int(w.StaleThreshold.Minutes())))
	if err == sql.ErrNoRows {
		return nil, nil // No jobs available
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}

	// Update job to processing
	updateQuery := `
		UPDATE reconciliation_jobs
		SET status = 'processing',
		    attempts = attempts + 1,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err = tx.Exec(updateQuery, job.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update job status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Claimed job: id=%s, batch_id=%s, file_path=%s", job.ID, job.BatchID, job.FilePath)
	return &job, nil
}

func (w *Worker) processJob(job *Job) {
	startTime := time.Now()
	log.Printf("Processing job: id=%s, batch_id=%s", job.ID, job.BatchID)

	// Update batch status to processing if not already
	_, err := w.DB.Exec(`
		UPDATE reconciliation_batches
		SET status = 'processing'
		WHERE id = $1 AND status = 'uploading'
	`, job.BatchID)
	if err != nil {
		log.Printf("Warning: Failed to update batch status: %v", err)
	}

	// Process the job (Step 8 will implement actual CSV processing)
	if w.ProcessJobFunc != nil {
		err = w.ProcessJobFunc(job)
	} else {
		// Placeholder: just mark as done for now
		log.Println("ProcessJobFunc not set - placeholder processing")
		err = nil
	}

	duration := time.Since(startTime)

	if err != nil {
		w.failJob(job, err, duration)
	} else {
		w.completeJob(job, duration)
	}
}

func (w *Worker) completeJob(job *Job, duration time.Duration) {
	tx, err := w.DB.Beginx()
	if err != nil {
		log.Printf("Error beginning transaction for job completion: %v", err)
		return
	}
	defer tx.Rollback()

	// Update job
	_, err = tx.Exec(`
		UPDATE reconciliation_jobs
		SET status = 'completed',
		    updated_at = NOW()
		WHERE id = $1
	`, job.ID)
	if err != nil {
		log.Printf("Error updating job status: %v", err)
		return
	}

	// Update batch to completed
	_, err = tx.Exec(`
		UPDATE reconciliation_batches
		SET status = 'completed',
		    completed_at = NOW()
		WHERE id = $1
	`, job.BatchID)
	if err != nil {
		log.Printf("Error updating batch status: %v", err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing job completion: %v", err)
		return
	}

	log.Printf("Job completed: id=%s, batch_id=%s, duration=%v", job.ID, job.BatchID, duration)
}

func (w *Worker) failJob(job *Job, err error, duration time.Duration) {
	errorMsg := err.Error()
	log.Printf("Job failed: id=%s, batch_id=%s, error=%s, duration=%v", job.ID, job.BatchID, errorMsg, duration)

	tx, err2 := w.DB.Beginx()
	if err2 != nil {
		log.Printf("Error beginning transaction for job failure: %v", err2)
		return
	}
	defer tx.Rollback()

	// Check if we should retry
	shouldRetry := job.Attempts+1 < w.MaxAttempts

	if shouldRetry {
		// Re-queue the job
		_, err2 = tx.Exec(`
			UPDATE reconciliation_jobs
			SET status = 'queued',
			    last_error = $1,
			    updated_at = NOW()
			WHERE id = $2
		`, errorMsg, job.ID)
	} else {
		// Mark as failed permanently
		_, err2 = tx.Exec(`
			UPDATE reconciliation_jobs
			SET status = 'failed',
			    last_error = $1,
			    updated_at = NOW()
			WHERE id = $2
		`, errorMsg, job.ID)

		// Mark batch as failed
		if err2 == nil {
			_, err2 = tx.Exec(`
				UPDATE reconciliation_batches
				SET status = 'failed',
				    completed_at = NOW()
				WHERE id = $1
			`, job.BatchID)
		}
	}

	if err2 != nil {
		log.Printf("Error updating job failure status: %v", err2)
		return
	}

	if err2 := tx.Commit(); err2 != nil {
		log.Printf("Error committing job failure: %v", err2)
		return
	}

	if shouldRetry {
		log.Printf("Job re-queued for retry: id=%s, attempts=%d", job.ID, job.Attempts+1)
	} else {
		log.Printf("Job failed permanently: id=%s, batch_id=%s", job.ID, job.BatchID)
	}
}

// UpdateBatchProgress updates batch counters (called during CSV processing)
// Uses direct query formatting to avoid prepared statement issues with Neon pooler
func (w *Worker) UpdateBatchProgress(batchID string, processed, autoMatched, needsReview, unmatched int) error {
	// Validate batchID is a valid UUID to prevent SQL injection
	if _, err := uuid.Parse(batchID); err != nil {
		return fmt.Errorf("invalid batch ID: %w", err)
	}
	
	// Format query directly to avoid prepared statements (safe since we validate UUID and use integers)
	// Use underlying *sql.DB to avoid sqlx's prepared statement handling
	query := fmt.Sprintf(`
		UPDATE reconciliation_batches
		SET processed_count = %d,
		    auto_matched_count = %d,
		    needs_review_count = %d,
		    unmatched_count = %d
		WHERE id = '%s'
	`, processed, autoMatched, needsReview, unmatched, batchID)
	
	_, err := w.DB.DB.Exec(query)
	return err
}

// SetBatchTotal sets total_transactions when processing completes
// Uses direct query formatting to avoid prepared statement issues with Neon pooler
func (w *Worker) SetBatchTotal(batchID string, total int) error {
	// Validate batchID is a valid UUID to prevent SQL injection
	if _, err := uuid.Parse(batchID); err != nil {
		return fmt.Errorf("invalid batch ID: %w", err)
	}
	
	// Format query directly to avoid prepared statements (safe since we validate UUID and use integers)
	// Use underlying *sql.DB to avoid sqlx's prepared statement handling
	query := fmt.Sprintf(`
		UPDATE reconciliation_batches
		SET total_transactions = %d
		WHERE id = '%s'
	`, total, batchID)
	
	_, err := w.DB.DB.Exec(query)
	return err
}

