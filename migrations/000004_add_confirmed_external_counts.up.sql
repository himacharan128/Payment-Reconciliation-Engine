-- Add confirmed_count and external_count to reconciliation_batches
ALTER TABLE reconciliation_batches 
ADD COLUMN confirmed_count INTEGER DEFAULT 0,
ADD COLUMN external_count INTEGER DEFAULT 0;

-- Initialize counts from existing transactions
UPDATE reconciliation_batches rb
SET 
    confirmed_count = (
        SELECT COUNT(*) 
        FROM bank_transactions 
        WHERE upload_batch_id = rb.id AND status = 'confirmed'
    ),
    external_count = (
        SELECT COUNT(*) 
        FROM bank_transactions 
        WHERE upload_batch_id = rb.id AND status = 'external'
    );

