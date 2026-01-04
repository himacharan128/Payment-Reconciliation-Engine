# Payment Reconciliation Engine

## Quickstart (Local)

### 1. Start Database
```bash
docker compose up -d
```

### 2. Set Up Environment
```bash
# Backend env (source into shell)
set -a
source .env.local
set +a

# Frontend env (Vite reads automatically from frontend/.env.local)
# Already configured
```

### 3. Seed Database
```bash
set -a; source .env.local; set +a
make seed
```

This loads 500 invoices from `seed/data/invoices.csv`. The command is idempotent - safe to run multiple times.

### 4. Run Services (3 terminals)

**Terminal 1 - API:**
```bash
set -a; source .env.local; set +a
make api
```

**Terminal 2 - Worker:**
```bash
set -a; source .env.local; set +a
make worker
```

**Terminal 3 - Frontend:**
```bash
make web
```

Then open:
- Frontend: http://localhost:5173
- API: http://localhost:8080

## Repo Structure
- backend/      Go (Echo) API + Worker
- frontend/     React UI
- migrations/   SQL migrations
- seed/         Seed scripts + sample CSVs
- docker/       Dockerfiles + nginx config
- data/uploads/ Uploaded CSVs (bind mount)

## Environment Configuration

### Local Development

**Backend** (root `.env.local`):
- `DATABASE_URL` - Postgres connection string
- `UPLOAD_DIR` - Directory for CSV uploads
- `CORS_ALLOWED_ORIGINS` - Allowed frontend origins
- `APP_ENV` - Environment identifier
- `JOB_POLL_INTERVAL_MS` - Worker polling interval
- `BATCH_PROGRESS_UPDATE_EVERY` - Progress update frequency

**Frontend** (`frontend/.env.local`):
- `VITE_API_URL` - Backend API URL (Vite reads at build time)

### Production

**Backend**: Set environment variables in hosting platform dashboard:
- `DATABASE_URL` - Cloud Postgres connection (with `sslmode=require`)
- `UPLOAD_DIR` - Persistent storage path (e.g., `/tmp/uploads` or object storage)
- `CORS_ALLOWED_ORIGINS` - Production frontend domain(s)
- `APP_ENV=production`
- Worker tuning variables

**Frontend**: Set `VITE_API_URL` in hosting platform's build environment settings.

### Important Notes

- **Vite env vars**: Only variables prefixed with `VITE_` are exposed to frontend
- **CORS**: Must match exact frontend origin (no wildcards in production)
- **Upload storage**: API and worker must share filesystem or use object storage
- **Never commit**: `.env.local`, `.env.production`, or any files with real secrets

## Seeding

The seed command (`make seed`) loads invoices from `seed/data/invoices.csv`:
- **Idempotent**: Safe to run multiple times (uses `ON CONFLICT DO NOTHING`)
- **Fast**: Batch inserts (200 rows per transaction)
- **Reports**: Shows parsed count, inserted count, skipped duplicates, and total DB count

To seed with a custom file:
```bash
cd backend && go run ./cmd/seed --file /path/to/invoices.csv
```

## Matching Algorithm

### Scoring Formula

The matching algorithm uses a multi-factor scoring system:

**Base Score = Name Similarity (0-100)**
- Uses Jaro-Winkler similarity algorithm
- Handles name variations (e.g., "SMITH JOHN" → "John Smith")
- Normalizes names: uppercase, remove punctuation, collapse spaces
- Extracts name from bank description by removing noise tokens (CHK, DEP, PMT, etc.)

**Date Adjustment (-10 to +5 points)**
- Transaction before due date: +5 points
- Transaction 0-7 days after due date: +2 points
- Transaction 8-30 days after: 0 points
- Transaction >30 days after: -10 points

**Ambiguity Penalty**
- If multiple invoices match the same amount: -(candidateCount - 1) × 2 points
- Prevents false positives when multiple invoices share an amount

**Final Score = Name Similarity + Date Adjustment - Ambiguity Penalty**
- Clamped to 0-100 range
- Rounded to 2 decimal places

### Confidence Thresholds

- **≥95.0**: `auto_matched` - System automatically matches
- **60.0-94.9**: `needs_review` - Requires admin confirmation
- **<60.0**: `unmatched` - No match found

### Match Details Schema (v1)

Each match includes detailed explanation in `match_details` JSONB field:

```json
{
  "version": "v1",
  "amount": {
    "transaction": "450.00",
    "invoice": "450.00"
  },
  "name": {
    "extracted": "SMITH JOHN",
    "invoiceName": "John David Smith",
    "similarity": 92.3
  },
  "date": {
    "transactionDate": "2024-12-15",
    "invoiceDueDate": "2024-12-10",
    "deltaDays": -5,
    "adjustment": 5.0
  },
  "ambiguity": {
    "candidateCount": 1,
    "penalty": 0.0
  },
  "finalScore": 97.3,
  "bucket": "auto_matched",
  "topCandidates": [...]
}
```

### Constraints

- **Amount must match exactly** - No match if amount differs
- **Paid invoices excluded** - Only `sent` and `overdue` invoices are considered
- **No duplicate matches** - Each invoice can only be matched once per batch
- **Deterministic tie-breaking** - When scores are equal, prefers:
  1. Smaller absolute date delta
  2. Earlier due date

## Notes
- This project includes async reconciliation processing (worker) and a dashboard UI.
- Seed data is available from `seed/data/` directory.

