#!/bin/bash

# Run database migration
# Usage: ./run_migration.sh [migration_file] [database_url]
# Example: ./run_migration.sh migrations/000004_add_confirmed_external_counts.up.sql
# Example: ./run_migration.sh migrations/000004_add_confirmed_external_counts.up.sql "postgres://user:pass@host/db"

# Check if DATABASE_URL is provided as argument
if [ ! -z "$2" ]; then
    DATABASE_URL="$2"
else
    # Try to load from environment files
    set -a
    source .env.local 2>/dev/null || source .env.production 2>/dev/null
    set +a
    
    # If still not set, prompt user
    if [ -z "$DATABASE_URL" ]; then
        echo "❌ DATABASE_URL not found in environment"
        echo ""
        echo "Please provide DATABASE_URL in one of these ways:"
        echo "1. As second argument: ./run_migration.sh migrations/000004_*.sql 'postgres://...'"
        echo "2. Set environment variable: export DATABASE_URL='postgres://...'"
        echo "3. Add to .env.local or .env.production file"
        echo ""
        echo "For production (Neon), get the URL from Render dashboard environment variables"
        exit 1
    fi
fi

MIGRATION_FILE="${1:-migrations/000004_add_confirmed_external_counts.up.sql}"

if [ ! -f "$MIGRATION_FILE" ]; then
    echo "Error: Migration file not found: $MIGRATION_FILE"
    exit 1
fi

echo "Running migration: $MIGRATION_FILE"
echo "Database: $(echo $DATABASE_URL | sed 's/:[^:]*@/:***@/')"
echo ""

# Use psql to run the migration
psql "$DATABASE_URL" -f "$MIGRATION_FILE"

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Migration completed successfully!"
else
    echo ""
    echo "❌ Migration failed!"
    echo ""
    echo "Troubleshooting:"
    echo "1. Check DATABASE_URL is correct"
    echo "2. Verify database is accessible"
    echo "3. Check if psql is installed: which psql"
    exit 1
fi

