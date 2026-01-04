-- Add file_content column to reconciliation_jobs for multi-instance compatibility
ALTER TABLE reconciliation_jobs
ADD COLUMN IF NOT EXISTS file_content BYTEA;

-- Add index for faster queries (optional, but helpful)
CREATE INDEX IF NOT EXISTS idx_reconciliation_jobs_file_content 
ON reconciliation_jobs(batch_id) 
WHERE file_content IS NOT NULL;

