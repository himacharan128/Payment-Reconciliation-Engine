-- Payment Reconciliation Engine Database Schema

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Invoices table
CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    invoice_number VARCHAR(255) UNIQUE NOT NULL,
    customer_name VARCHAR(255) NOT NULL,
    customer_email VARCHAR(255),
    amount DECIMAL(10,2) NOT NULL,
    status VARCHAR(50) NOT NULL CHECK (status IN ('draft', 'sent', 'paid', 'overdue')),
    due_date DATE NOT NULL,
    paid_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Indexes for invoices
CREATE INDEX IF NOT EXISTS idx_invoices_customer_name ON invoices(customer_name);
CREATE INDEX IF NOT EXISTS idx_invoices_amount ON invoices(amount);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_due_date ON invoices(due_date);
CREATE INDEX IF NOT EXISTS idx_invoices_invoice_number ON invoices(invoice_number);

-- Full-text search index for customer name
CREATE INDEX IF NOT EXISTS idx_invoices_customer_name_gin ON invoices USING gin(to_tsvector('english', customer_name));

-- Reconciliation batches table
CREATE TABLE IF NOT EXISTS reconciliation_batches (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    filename VARCHAR(255) NOT NULL,
    total_transactions INTEGER DEFAULT 0,
    processed_count INTEGER DEFAULT 0,
    auto_matched_count INTEGER DEFAULT 0,
    needs_review_count INTEGER DEFAULT 0,
    unmatched_count INTEGER DEFAULT 0,
    status VARCHAR(50) NOT NULL CHECK (status IN ('uploading', 'processing', 'completed', 'failed')),
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Bank transactions table
CREATE TABLE IF NOT EXISTS bank_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    upload_batch_id UUID NOT NULL REFERENCES reconciliation_batches(id) ON DELETE CASCADE,
    transaction_date DATE NOT NULL,
    description TEXT NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    reference_number VARCHAR(255),
    status VARCHAR(50) NOT NULL CHECK (status IN ('pending', 'auto_matched', 'needs_review', 'unmatched', 'confirmed', 'external')),
    matched_invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    confidence_score DECIMAL(5,2),
    match_details JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Indexes for bank_transactions
CREATE INDEX IF NOT EXISTS idx_bank_transactions_batch ON bank_transactions(upload_batch_id);
CREATE INDEX IF NOT EXISTS idx_bank_transactions_status ON bank_transactions(status);
CREATE INDEX IF NOT EXISTS idx_bank_transactions_matched_invoice ON bank_transactions(matched_invoice_id);
CREATE INDEX IF NOT EXISTS idx_bank_transactions_amount ON bank_transactions(amount);
CREATE INDEX IF NOT EXISTS idx_bank_transactions_date ON bank_transactions(transaction_date);

-- Note: Multiple transactions CAN match the same invoice
-- This is valid for: duplicate payments, partial payments, same customer paying multiple times
-- The system flags these as "needs_review" for human verification

-- Reconciliation jobs table (for worker)
CREATE TABLE IF NOT EXISTS reconciliation_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    batch_id UUID NOT NULL REFERENCES reconciliation_batches(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    file_content BYTEA,
    status VARCHAR(50) NOT NULL CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
    attempts INTEGER DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Indexes for reconciliation_jobs
CREATE INDEX IF NOT EXISTS idx_jobs_status ON reconciliation_jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_batch ON reconciliation_jobs(batch_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON reconciliation_jobs(created_at);

-- Match audit log table
CREATE TABLE IF NOT EXISTS match_audit_log (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id UUID NOT NULL REFERENCES bank_transactions(id) ON DELETE CASCADE,
    action VARCHAR(50) NOT NULL CHECK (action IN ('auto_matched', 'confirmed', 'rejected', 'manual_matched', 'marked_external')),
    previous_invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    new_invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
    performed_by VARCHAR(255) NOT NULL,
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Indexes for match_audit_log
CREATE INDEX IF NOT EXISTS idx_audit_transaction ON match_audit_log(transaction_id);
CREATE INDEX IF NOT EXISTS idx_audit_action ON match_audit_log(action);
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON match_audit_log(created_at);

