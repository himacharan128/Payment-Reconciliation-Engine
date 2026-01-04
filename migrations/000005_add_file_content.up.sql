-- Add file_content column to store CSV file content
-- This allows API and Worker on different instances to share files via database
ALTER TABLE reconciliation_jobs
ADD COLUMN IF NOT EXISTS file_content BYTEA;

