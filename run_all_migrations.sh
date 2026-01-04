#!/bin/bash

# Run all required migrations
# Usage: ./run_all_migrations.sh [database_url]
# Example: ./run_all_migrations.sh "postgres://user:pass@host/db"

# Check if DATABASE_URL is provided as argument
if [ ! -z "$1" ]; then
    DATABASE_URL="$1"
else
    # Try to load from environment files
    set -a
    source .env.local 2>/dev/null || source .env.production 2>/dev/null
    set +a
    
    # If still not set, prompt user
    if [ -z "$DATABASE_URL" ]; then
        echo "❌ DATABASE_URL not found"
        echo ""
        echo "Usage: ./run_all_migrations.sh 'postgres://user:pass@host/db'"
        echo "Or set: export DATABASE_URL='postgres://...'"
        echo ""
        echo "For production (Neon), get URL from Render dashboard"
        exit 1
    fi
fi

echo "🔧 Running all required migrations..."
echo "Database: $(echo $DATABASE_URL | sed 's/:[^:]*@/:***@/')"
echo ""

# Migration 1: confirmed_count and external_count
echo "1. Running migration 000004 (confirmed_count, external_count)..."
psql "$DATABASE_URL" -f migrations/000004_add_confirmed_external_counts.up.sql
if [ $? -ne 0 ]; then
    echo "❌ Migration 000004 failed!"
    exit 1
fi
echo "✅ Migration 000004 completed"
echo ""

# Migration 2: file_content column
echo "2. Running migration 000005 (file_content)..."
psql "$DATABASE_URL" -f migrations/000005_add_file_content_column.up.sql
if [ $? -ne 0 ]; then
    echo "❌ Migration 000005 failed!"
    exit 1
fi
echo "✅ Migration 000005 completed"
echo ""

echo "=========================================="
echo "✅ All migrations completed successfully!"
echo ""
echo "Next steps:"
echo "1. Redeploy API and Worker services"
echo "2. Run: ./verify_deployment.sh YOUR_API_URL"

