-- Remove file_content column
ALTER TABLE reconciliation_jobs
DROP COLUMN IF EXISTS file_content;

DROP INDEX IF EXISTS idx_reconciliation_jobs_file_content;

