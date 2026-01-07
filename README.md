# Payment Reconciliation Engine

Automatically matches bank transactions to invoices using fuzzy name matching. Built with Go + React.

**Live Demo:** https://payment-reconciliation-engine.vercel.app/reconciliation/new

---

## Setup Instructions

### Prerequisites
- Docker (for PostgreSQL)
- Go 1.21+
- Node.js 18+

### Quick Start

```bash
# 1. Start database
docker compose up -d

# 2. Run migrations
export DATABASE_URL="postgres://app:app@localhost:5432/app?sslmode=disable"
./run_all_migrations.sh "$DATABASE_URL"

# 3. Seed invoices
make seed

# 4. Start services (3 terminals)
make api      # Terminal 1: API on :8080
make worker   # Terminal 2: Background processor
make web      # Terminal 3: Frontend on :5173
```

### Environment Variables

**Backend** (`.env.local`):
```
DATABASE_URL=postgres://app:app@localhost:5432/app?sslmode=disable
```

**Frontend** (`frontend/.env.local`):
```
VITE_API_URL=http://localhost:8080
```

---

## Technical Decisions

### 1. Matching Algorithm

**Problem:** Bank descriptions are messy. "SMITH JOHN CHK DEP" needs to match "John D. Smith".

**Solution:** Enhanced Jaro-Winkler with three matching strategies:

1. **Token-sorted comparison** - Sorts words alphabetically before comparing. "SMITH JOHN" and "JOHN SMITH" both become "JOHN SMITH" → 100% match.

2. **Token overlap scoring** - Handles initials. "S ADAMS" matches "SARAH ADAMS" because "S" matches the first letter of "SARAH".

3. **Standard Jaro-Winkler** - Character-level similarity for fuzzy matching.

The algorithm picks the highest score from all three methods.

**Scoring formula:**
```
Final Score = Name Similarity (0-100) + Date Adjustment (-5 to +5) - Ambiguity Penalty
```

**Thresholds (per BRD):**
- ≥95% → `auto_matched`
- 60-94% → `needs_review`  
- <60% → `unmatched`

**Why this works:** Standard Jaro-Winkler fails on name order swaps (only 53% for "SMITH JOHN" vs "JOHN SMITH"). Token-sorted matching fixes this by normalizing word order first.

### 2. Large CSV Processing

**Problem:** 10,000+ transactions can't be loaded into memory at once.

**Solution:** Streaming + batching:

- **Streaming CSV parser** - Go's `csv.Reader` processes one row at a time. Memory stays constant regardless of file size.
- **Batch inserts** - Transactions are inserted 500 at a time using multi-row `INSERT`. This reduces 10,000 database calls to 20.
- **In-memory invoice cache** - All eligible invoices (sent/overdue) are loaded once at job start and indexed by amount. Matching is O(1) lookup + scoring.

### 3. Background Jobs

**Problem:** Processing 10K transactions takes ~15 seconds. Can't block the HTTP request.

**Solution:** Database-backed job queue:

1. Upload creates a job in `reconciliation_jobs` table (status: `queued`)
2. Worker polls for jobs using `SELECT ... FOR UPDATE SKIP LOCKED`
3. Worker processes CSV, updates progress every 200 rows
4. Frontend polls `GET /api/reconciliation/:batchId` for status

**Why not Redis/RabbitMQ?** PostgreSQL is already required. One less dependency to manage. `SKIP LOCKED` handles concurrent workers safely.

### 4. Invoice Search

**Problem:** Admins need to search 500+ invoices by name/amount/invoice number. Must be <200ms.

**Solution:** PostgreSQL trigram indexes (`pg_trgm`):

```sql
CREATE INDEX idx_invoices_customer_name_trgm ON invoices 
  USING GIN (customer_name gin_trgm_ops);
```

This makes `ILIKE '%smith%'` queries fast. Combined with B-tree indexes on `amount` and `status` for exact filters.

**Result:** ~50-150ms backend response time.

### 5. Pagination

**Problem:** `OFFSET 9000` on a large table is slow (scans 9000 rows to skip them).

**Solution:** Cursor-based pagination using `(created_at, id)`:

```sql
WHERE (created_at, id) < ($cursor_time, $cursor_id)
ORDER BY created_at DESC, id DESC
LIMIT 50
```

This is O(1) regardless of page position because it uses the index directly.

---

## Trade-offs & Limitations

**What I'd improve with more time:**
- WebSocket/SSE instead of polling for real-time progress
- ML-based matching using historical confirmed matches
- Reference number matching as additional signal
- Export Unmatched: Currently uses client-side CSV generation from fetched transactions. A dedicated backend endpoint would be more efficient for very large datasets

**Scaling limits:**
- Current: ~10K transactions/batch comfortably
- Bottleneck: Single worker. Would add worker pool for >100K/batch
- Large CSVs (>10MB): Stored in database which adds latency. Would use S3 for production.

**Known issues:**
- Render free tier spins down after 15min inactivity (cold starts)
- Progress updates every 200 rows can feel slow on very large batches

---

## Performance Results

| Metric | Result | Target |
|--------|--------|--------|
| 1,000 transactions | **~5 seconds** | <30 seconds ✅ |
| 10,000 transactions | **~15 seconds** | - ✅ |
| Invoice search | **~100ms** | <200ms ✅ |
| Bulk confirm | **<1 second** | <5 seconds ✅ |
---