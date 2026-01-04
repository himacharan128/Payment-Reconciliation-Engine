# Deployment Guide

This guide covers deploying the Payment Reconciliation Engine to production:
- **Backend (API + Worker)**: Render
- **Frontend**: Vercel
- **Database**: PostgreSQL (Render, Supabase, or Neon)

## Folder Structure for Deployment

Your current monorepo structure is **perfectly fine** for separate deployments! Here's how:

### ✅ Current Structure (Keep It!)
```
payment-reconciliation-engine/
├── backend/          # Go API + Worker
├── frontend/         # React app
├── migrations/       # SQL migrations (shared)
├── seed/            # Seed scripts
└── README.md
```

**Why this works:**
- Both Render and Vercel can deploy from a monorepo
- You specify the **root directory** in each service's settings
- Migrations can be accessed from root during deployment

---

## Database Hosting Options

### Option 1: Render PostgreSQL (Recommended for Render backend)
- **Pros**: Same platform, easy connection, automatic backups
- **Cons**: Slightly more expensive
- **Setup**: Create PostgreSQL service in Render dashboard

### Option 2: Supabase (Free tier available)
- **Pros**: Free tier, great developer experience, built-in migrations tool
- **Cons**: Different platform
- **Setup**: Create project → Get connection string → Use in Render

### Option 3: Neon (Serverless Postgres)
- **Pros**: Serverless, auto-scaling, free tier
- **Cons**: Newer platform
- **Setup**: Create project → Get connection string

**Recommendation**: Use **Render PostgreSQL** if deploying backend on Render (simplest setup).

---

## Backend Deployment (Render)

### Step 1: Create Two Services in Render

#### Service 1: API
1. **New** → **Web Service**
2. **Connect** your GitHub repo
3. **Settings**:
   - **Name**: `payment-reconciliation-api`
   - **Root Directory**: `backend` ⚠️ **Important!**
   - **Environment**: `Go`
   - **Build Command**: `go build -o api ./cmd/api`
   - **Start Command**: `./api`
   - **Port**: `8080` (or set `PORT` env var)

#### Service 2: Worker (Background Worker)
1. **New** → **Background Worker**
2. **Connect** same GitHub repo
3. **Settings**:
   - **Name**: `payment-reconciliation-worker`
   - **Root Directory**: `backend` ⚠️ **Important!**
   - **Environment**: `Go`
   - **Build Command**: `go build -o worker ./cmd/worker`
   - **Start Command**: `./worker`

### Step 2: Environment Variables (Both Services)

Set these in Render dashboard for **both API and Worker**:

```bash
# Database
DATABASE_URL=postgresql://user:pass@host:5432/dbname?sslmode=require

# Upload directory (use Render's disk)
UPLOAD_DIR=/opt/render/project/src/backend/data/uploads

# CORS (set after frontend is deployed)
CORS_ALLOWED_ORIGINS=https://your-frontend.vercel.app

# Port (for API only)
PORT=8080

# Worker settings (optional)
JOB_POLL_INTERVAL_MS=1000
BATCH_PROGRESS_UPDATE_EVERY=50
```

### Step 3: Handle Migrations

**Option A: Run migrations manually** (Recommended for first deploy)
```bash
# Install migrate tool locally or use Docker
docker run -v $(pwd)/migrations:/migrations --network host migrate/migrate \
  -path /migrations -database "postgresql://..." up
```

**Option B: Add migration step to Render build**
Create `backend/render-build.sh`:
```bash
#!/bin/bash
# Install migrate tool
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Run migrations (from repo root)
cd ..
migrate -path migrations -database "$DATABASE_URL" up
```

Then set **Build Command** to: `bash render-build.sh && go build -o api ./cmd/api`

**Option C: Use Supabase migrations** (if using Supabase)
- Supabase has built-in migration runner in dashboard

### Step 4: File Storage

**Important**: Render's disk is **ephemeral** (cleared on deploy). For production:

**Option 1: Use object storage** (Recommended)
- AWS S3, Google Cloud Storage, or Cloudflare R2
- Update upload handler to use S3 SDK
- Set `UPLOAD_DIR` to S3 bucket path

**Option 2: Use Render Disk** (Temporary)
- Files persist between restarts but not across deploys
- Good for testing, not production

**Option 3: Store in database** (Small files only)
- Add `file_content BYTEA` column
- Not recommended for large CSVs

---

## Frontend Deployment (Vercel)

### Step 1: Connect Repository
1. Go to Vercel dashboard
2. **Add New Project**
3. Import your GitHub repo

### Step 2: Configure Project
- **Framework Preset**: `Vite`
- **Root Directory**: `frontend` ⚠️ **Important!**
- **Build Command**: `npm run build` (default)
- **Output Directory**: `dist` (default)
- **Install Command**: `npm install` (default)

### Step 3: Environment Variables
Set in Vercel dashboard:
```bash
VITE_API_URL=https://your-api.onrender.com
```

### Step 4: Deploy
Click **Deploy** - Vercel will:
1. Install dependencies
2. Build the app
3. Deploy to CDN

---

## Deployment Checklist

### Pre-Deployment
- [ ] Database created and migrations run
- [ ] Environment variables prepared
- [ ] CORS origins configured
- [ ] File storage solution chosen

### Backend (Render)
- [ ] API service created with correct root directory
- [ ] Worker service created with correct root directory
- [ ] Environment variables set for both services
- [ ] Migrations run (manually or via build script)
- [ ] Health check endpoint working (`/health`)

### Frontend (Vercel)
- [ ] Project created with `frontend` root directory
- [ ] `VITE_API_URL` environment variable set
- [ ] Build succeeds
- [ ] Frontend can reach backend API

### Post-Deployment
- [ ] Test file upload
- [ ] Test transaction matching
- [ ] Test manual actions (confirm, reject, match)
- [ ] Monitor logs for errors
- [ ] Set up error tracking (Sentry, etc.)

---

## Troubleshooting

### "Migrations folder not found"
- **Cause**: Root directory set to `backend/` but migrations are at root
- **Fix**: Either:
  1. Copy migrations to `backend/migrations/` and update paths
  2. Set root directory to repo root and adjust build commands
  3. Use absolute paths in migration commands

### "Cannot connect to database"
- Check `DATABASE_URL` format: `postgresql://user:pass@host:port/db?sslmode=require`
- Verify database allows connections from Render IPs
- Check firewall rules

### "CORS errors"
- Ensure `CORS_ALLOWED_ORIGINS` matches **exact** frontend URL (no trailing slash)
- For multiple origins: `https://app1.vercel.app,https://app2.vercel.app`

### "File uploads not persisting"
- Render disk is ephemeral - use object storage for production
- Or implement database storage for small files

### "Worker not processing jobs"
- Check worker logs in Render dashboard
- Verify `DATABASE_URL` is set correctly
- Ensure worker service is running (not paused)

---

## Alternative: Single Root Directory Approach

If you prefer deploying from repo root:

### Render API Settings:
- **Root Directory**: `.` (root)
- **Build Command**: `cd backend && go build -o api ./cmd/api`
- **Start Command**: `cd backend && ./api`

### Render Worker Settings:
- **Root Directory**: `.` (root)
- **Build Command**: `cd backend && go build -o worker ./cmd/worker`
- **Start Command**: `cd backend && ./worker`

### Vercel Settings:
- **Root Directory**: `frontend`
- (No changes needed)

This approach gives access to `migrations/` folder from root, but requires `cd` commands in build/start.

---

## Cost Estimates

### Render
- **API**: Free tier (spins down after 15min inactivity) or $7/month (always on)
- **Worker**: Free tier (spins down) or $7/month (always on)
- **PostgreSQL**: $7/month (starter) or $20/month (standard)

### Vercel
- **Frontend**: Free tier (generous limits) or $20/month (Pro)

### Total: ~$14-34/month for production setup

---

## Next Steps

1. **Choose database provider** (Render PostgreSQL recommended)
2. **Create Render services** (API + Worker)
3. **Deploy frontend to Vercel**
4. **Run migrations** on production database
5. **Test end-to-end** workflow
6. **Set up monitoring** (logs, errors, metrics)

