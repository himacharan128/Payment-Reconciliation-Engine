-- Extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Enum Types
CREATE TYPE invoice_status AS ENUM ('draft', 'sent', 'paid', 'overdue');
CREATE TYPE transaction_status AS ENUM ('pending', 'auto_matched', 'needs_review', 'unmatched', 'confirmed', 'external');
CREATE TYPE batch_status AS ENUM ('uploading', 'processing', 'completed', 'failed');
CREATE TYPE audit_action AS ENUM ('auto_matched', 'confirmed', 'rejected', 'manual_matched', 'marked_external');

-- Tables

-- invoices table
CREATE TABLE invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_number VARCHAR(255) NOT NULL UNIQUE,
    customer_name VARCHAR(255) NOT NULL,
    customer_email VARCHAR(255),
    amount NUMERIC(10,2) NOT NULL,
    status invoice_status NOT NULL,
    due_date DATE NOT NULL,
    paid_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- reconciliation_batches table
CREATE TABLE reconciliation_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    filename VARCHAR(255) NOT NULL,
    total_transactions INTEGER DEFAULT 0,
    processed_count INTEGER DEFAULT 0,
    auto_matched_count INTEGER DEFAULT 0,
    needs_review_count INTEGER DEFAULT 0,
    unmatched_count INTEGER DEFAULT 0,
    status batch_status NOT NULL DEFAULT 'uploading',
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- bank_transactions table
CREATE TABLE bank_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_batch_id UUID NOT NULL REFERENCES reconciliation_batches(id) ON DELETE CASCADE,
    transaction_date DATE NOT NULL,
    description TEXT NOT NULL,
    amount NUMERIC(10,2) NOT NULL,
    reference_number VARCHAR(255),
    status transaction_status NOT NULL DEFAULT 'pending',
    matched_invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    confidence_score NUMERIC(5,2), -- 0.00 to 100.00
    match_details JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- match_audit_logs table
CREATE TABLE match_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES bank_transactions(id) ON DELETE CASCADE,
    action audit_action NOT NULL,
    previous_invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    new_invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    performed_by VARCHAR(255) NOT NULL, -- 'system' or user identifier
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Indexes for performance

-- Dashboard tabs + cursor pagination
-- Supports "All" tab with cursor pagination
CREATE INDEX idx_bank_transactions_batch_created_id 
    ON bank_transactions(upload_batch_id, created_at DESC, id DESC);

-- Supports filtered tabs (status) + cursor pagination
CREATE INDEX idx_bank_transactions_batch_status_created_id 
    ON bank_transactions(upload_batch_id, status, created_at DESC, id DESC);

-- Matching speed: amount lookup for invoice matching
CREATE INDEX idx_invoices_amount ON invoices(amount);

-- Supports filtering unpaid/eligible invoices during matching
CREATE INDEX idx_invoices_amount_status ON invoices(amount, status) 
    WHERE status IN ('sent', 'overdue');

-- Foreign key indexes (for join performance)
CREATE INDEX idx_bank_transactions_matched_invoice 
    ON bank_transactions(matched_invoice_id) 
    WHERE matched_invoice_id IS NOT NULL;

CREATE INDEX idx_match_audit_logs_transaction 
    ON match_audit_logs(transaction_id);

CREATE INDEX idx_match_audit_logs_created 
    ON match_audit_logs(created_at DESC);

-- Reconciliation batch lookups
CREATE INDEX idx_reconciliation_batches_status 
    ON reconciliation_batches(status);

CREATE INDEX idx_reconciliation_batches_created 
    ON reconciliation_batches(created_at DESC);

