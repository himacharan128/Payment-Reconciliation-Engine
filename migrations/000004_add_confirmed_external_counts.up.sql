-- Add confirmed_count and external_count columns to reconciliation_batches
ALTER TABLE reconciliation_batches
ADD COLUMN IF NOT EXISTS confirmed_count INTEGER DEFAULT 0,
ADD COLUMN IF NOT EXISTS external_count INTEGER DEFAULT 0;

-- Initialize counts from existing transactions (if any)
UPDATE reconciliation_batches rb
SET 
    confirmed_count = COALESCE((
        SELECT COUNT(*) 
        FROM bank_transactions 
        WHERE upload_batch_id = rb.id AND status = 'confirmed'
    ), 0),
    external_count = COALESCE((
        SELECT COUNT(*) 
        FROM bank_transactions 
        WHERE upload_batch_id = rb.id AND status = 'external'
    ), 0)
WHERE confirmed_count IS NULL OR external_count IS NULL;
