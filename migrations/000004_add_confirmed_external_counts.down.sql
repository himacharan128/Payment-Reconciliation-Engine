-- Remove confirmed_count and external_count columns
ALTER TABLE reconciliation_batches 
DROP COLUMN IF EXISTS confirmed_count,
DROP COLUMN IF EXISTS external_count;

