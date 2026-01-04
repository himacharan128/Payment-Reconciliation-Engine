-- Remove file_content column
ALTER TABLE reconciliation_jobs
DROP COLUMN IF EXISTS file_content;

