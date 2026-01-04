-- Add confirmed_count and external_count columns to reconciliation_batches
ALTER TABLE reconciliation_batches
ADD COLUMN IF NOT EXISTS confirmed_count INTEGER DEFAULT 0,
ADD COLUMN IF NOT EXISTS external_count INTEGER DEFAULT 0;

-- Update existing rows to have 0 for these counts
UPDATE reconciliation_batches
SET confirmed_count = 0, external_count = 0
WHERE confirmed_count IS NULL OR external_count IS NULL;
