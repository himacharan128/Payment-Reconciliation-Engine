-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS match_audit_logs;
DROP TABLE IF EXISTS bank_transactions;
DROP TABLE IF EXISTS reconciliation_batches;
DROP TABLE IF EXISTS invoices;

-- Drop enum types
DROP TYPE IF EXISTS audit_action;
DROP TYPE IF EXISTS batch_status;
DROP TYPE IF EXISTS transaction_status;
DROP TYPE IF EXISTS invoice_status;

-- Extensions are usually kept, but can be dropped if needed
-- DROP EXTENSION IF EXISTS "pgcrypto";

