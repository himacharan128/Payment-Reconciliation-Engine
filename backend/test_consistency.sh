#!/bin/bash

# =============================================================================
# Payment Reconciliation Engine - Consistency Test Script
# =============================================================================
# This script tests that the matching engine produces consistent results
# across multiple runs with the same input data.
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
NUM_TESTS=${1:-10}  # Number of tests to run (default: 10)
WAIT_TIME=8         # Seconds to wait for job processing
DB_CONTAINER="payment-reconciliation-db"
DB_USER="postgres"
DB_NAME="payment_reconciliation"
CSV_FILE="backend/data/seed/bank_transactions_large.csv"
API_PORT=8080

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${BLUE}==============================================================================${NC}"
echo -e "${BLUE}  Payment Reconciliation Engine - Consistency Test${NC}"
echo -e "${BLUE}==============================================================================${NC}"
echo ""

# -----------------------------------------------------------------------------
# Step 1: Kill ALL stale processes (including Go build cache processes)
# -----------------------------------------------------------------------------
echo -e "${YELLOW}[Step 1] Killing all stale processes...${NC}"

pkill -9 -f "go-build" 2>/dev/null || true
pkill -9 -f "/var/folders.*exe/main" 2>/dev/null || true
pkill -9 -f "run_worker" 2>/dev/null || true
pkill -9 -f "run_api" 2>/dev/null || true
pkill -9 -f "/tmp/run_" 2>/dev/null || true
lsof -ti:${API_PORT} | xargs kill -9 2>/dev/null || true
sleep 3

# Verify all killed
REMAINING=$(ps aux | grep -E "run_worker|run_api|go-build.*main" | grep -v grep | wc -l)
if [ "$REMAINING" -gt 0 ]; then
    echo -e "${RED}Warning: Some processes may still be running${NC}"
    ps aux | grep -E "run_worker|run_api|go-build.*main" | grep -v grep
else
    echo -e "${GREEN}All stale processes killed${NC}"
fi

# -----------------------------------------------------------------------------
# Step 2: Clear Go build cache and rebuild
# -----------------------------------------------------------------------------
echo ""
echo -e "${YELLOW}[Step 2] Clearing Go build cache and rebuilding...${NC}"

go clean -cache 2>/dev/null || true
rm -f /tmp/run_api /tmp/run_worker

echo "Building API server..."
go build -a -o /tmp/run_api ./cmd/api
echo "Building Worker..."
go build -a -o /tmp/run_worker ./cmd/worker

echo -e "${GREEN}Build completed${NC}"

# -----------------------------------------------------------------------------
# Step 3: Set up environment and start services
# -----------------------------------------------------------------------------
echo ""
echo -e "${YELLOW}[Step 3] Starting services...${NC}"

export DATABASE_URL="postgresql://${DB_USER}:${DB_USER}@localhost:5432/${DB_NAME}?sslmode=disable"
export PORT=${API_PORT}

# Create log directory
mkdir -p /tmp/reconciliation_tests
LOG_FILE="/tmp/reconciliation_tests/test_$(date +%Y%m%d_%H%M%S).log"

# Start API
/tmp/run_api > /tmp/reconciliation_tests/api.log 2>&1 &
API_PID=$!
sleep 2

# Start Worker
/tmp/run_worker > /tmp/reconciliation_tests/worker.log 2>&1 &
WORKER_PID=$!
sleep 3

# Verify services started
if ! ps -p $API_PID > /dev/null 2>&1; then
    echo -e "${RED}ERROR: API failed to start${NC}"
    cat /tmp/reconciliation_tests/api.log
    exit 1
fi

if ! ps -p $WORKER_PID > /dev/null 2>&1; then
    echo -e "${RED}ERROR: Worker failed to start${NC}"
    cat /tmp/reconciliation_tests/worker.log
    exit 1
fi

echo -e "${GREEN}Services started (API PID: $API_PID, Worker PID: $WORKER_PID)${NC}"

# -----------------------------------------------------------------------------
# Step 4: Run consistency tests
# -----------------------------------------------------------------------------
echo ""
echo -e "${YELLOW}[Step 4] Running ${NUM_TESTS} consistency tests...${NC}"
echo ""

# Arrays to store results
declare -a RESULTS
declare -a AUTO_COUNTS
FIRST_RESULT=""
ALL_CONSISTENT=true

for i in $(seq 1 $NUM_TESTS); do
    # Truncate tables
    docker exec $DB_CONTAINER psql -U $DB_USER -d $DB_NAME \
        -c "TRUNCATE bank_transactions, reconciliation_batches, reconciliation_jobs CASCADE;" \
        > /dev/null 2>&1
    sleep 1
    
    # Upload CSV file
    RESPONSE=$(curl -s -X POST "http://localhost:${API_PORT}/api/reconciliation/upload" \
        -F "file=@${CSV_FILE}")
    
    BATCH_ID=$(echo $RESPONSE | grep -o '"batchId":"[^"]*"' | cut -d'"' -f4)
    
    if [ -z "$BATCH_ID" ]; then
        echo -e "${RED}Test $i: FAILED - Could not upload file${NC}"
        RESULTS[$i]="UPLOAD_FAILED"
        ALL_CONSISTENT=false
        continue
    fi
    
    # Wait for job to complete
    for j in $(seq 1 20); do
        STATUS=$(docker exec $DB_CONTAINER psql -U $DB_USER -d $DB_NAME -t \
            -c "SELECT status FROM reconciliation_batches WHERE id='$BATCH_ID';" 2>/dev/null | xargs)
        if [ "$STATUS" = "completed" ]; then
            break
        fi
        sleep 0.5
    done
    
    if [ "$STATUS" != "completed" ]; then
        echo -e "${RED}Test $i: FAILED - Job did not complete (status: $STATUS)${NC}"
        RESULTS[$i]="NOT_COMPLETED"
        ALL_CONSISTENT=false
        continue
    fi
    
    # Get results
    RESULT=$(docker exec $DB_CONTAINER psql -U $DB_USER -d $DB_NAME -t \
        -c "SELECT 'auto=' || auto_matched_count || ', review=' || needs_review_count || ', unmatched=' || unmatched_count FROM reconciliation_batches WHERE id='$BATCH_ID';" \
        | xargs)
    
    AUTO=$(docker exec $DB_CONTAINER psql -U $DB_USER -d $DB_NAME -t \
        -c "SELECT auto_matched_count FROM reconciliation_batches WHERE id='$BATCH_ID';" | xargs)
    
    RESULTS[$i]="$RESULT"
    AUTO_COUNTS[$i]="$AUTO"
    
    # Check consistency
    if [ -z "$FIRST_RESULT" ]; then
        FIRST_RESULT="$RESULT"
        echo -e "${GREEN}Test $i: $RESULT${NC}"
    elif [ "$RESULT" = "$FIRST_RESULT" ]; then
        echo -e "${GREEN}Test $i: $RESULT ✓${NC}"
    else
        echo -e "${RED}Test $i: $RESULT ✗ (differs from test 1)${NC}"
        ALL_CONSISTENT=false
    fi
done

# -----------------------------------------------------------------------------
# Step 5: Summary
# -----------------------------------------------------------------------------
echo ""
echo -e "${BLUE}==============================================================================${NC}"
echo -e "${BLUE}  Test Summary${NC}"
echo -e "${BLUE}==============================================================================${NC}"
echo ""

# Count unique results
UNIQUE_RESULTS=$(printf '%s\n' "${RESULTS[@]}" | sort -u | wc -l)

echo "Total tests run: $NUM_TESTS"
echo "Unique result patterns: $UNIQUE_RESULTS"
echo ""

if [ "$ALL_CONSISTENT" = true ]; then
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║  ✅ ALL TESTS PASSED - Results are CONSISTENT!               ║${NC}"
    echo -e "${GREEN}║  Result: $FIRST_RESULT${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
else
    echo -e "${RED}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║  ❌ TESTS FAILED - Results are INCONSISTENT!                  ║${NC}"
    echo -e "${RED}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo "Result distribution:"
    printf '%s\n' "${RESULTS[@]}" | sort | uniq -c | sort -rn
fi

echo ""

# -----------------------------------------------------------------------------
# Step 6: Show debug info from worker log
# -----------------------------------------------------------------------------
echo -e "${YELLOW}[Debug Info] Invoice cache order verification:${NC}"
grep "DEBUG: DB returned" /tmp/reconciliation_tests/worker.log 2>/dev/null | head -3 || echo "No debug logs found"

echo ""
echo -e "${YELLOW}[Debug Info] Processing results from worker:${NC}"
grep "Processing complete" /tmp/reconciliation_tests/worker.log 2>/dev/null | tail -5 || echo "No processing logs found"

# -----------------------------------------------------------------------------
# Cleanup (optional - comment out to keep services running)
# -----------------------------------------------------------------------------
echo ""
echo -e "${YELLOW}[Cleanup] Stopping services...${NC}"
kill $API_PID 2>/dev/null || true
kill $WORKER_PID 2>/dev/null || true
echo -e "${GREEN}Done!${NC}"

# Exit with appropriate code
if [ "$ALL_CONSISTENT" = true ]; then
    exit 0
else
    exit 1
fi

