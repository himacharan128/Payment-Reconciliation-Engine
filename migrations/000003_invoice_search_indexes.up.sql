-- Enable pg_trgm extension for fuzzy text search
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Create GIN trigram index on customer_name for fuzzy name search
CREATE INDEX idx_invoices_customer_name_trgm ON invoices USING GIN (customer_name gin_trgm_ops);

-- Create trigram index on invoice_number for partial invoice number searches
CREATE INDEX idx_invoices_invoice_number_trgm ON invoices USING GIN (invoice_number gin_trgm_ops);

