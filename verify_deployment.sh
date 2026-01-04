#!/bin/bash

# Comprehensive deployment verification script
# Tests all critical functionality

set -e

BASE_URL="${1:-http://localhost:8080}"
echo "🔍 Verifying deployment: $BASE_URL"
echo "=========================================="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test 1: Health check / API availability
echo "1. Testing API availability..."
if curl -s -f "$BASE_URL/api/reconciliation/new" > /dev/null 2>&1 || curl -s -f "$BASE_URL/debug/routes" > /dev/null 2>&1; then
    echo -e "   ${GREEN}✅ API is responding${NC}"
else
    echo -e "   ${RED}❌ API is not responding${NC}"
    exit 1
fi
echo ""

# Test 2: Check if we can get a batch (if one exists)
echo "2. Testing batch status endpoint structure..."
echo "   Please provide a batch ID (or press Enter to skip):"
read -p "   Batch ID: " BATCH_ID

if [ ! -z "$BATCH_ID" ]; then
    echo ""
    echo "   Fetching batch status..."
    RESPONSE=$(curl -s "$BASE_URL/api/reconciliation/$BATCH_ID")
    
    if echo "$RESPONSE" | grep -q '"error"'; then
        echo -e "   ${RED}❌ Error in response:${NC}"
        echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
    else
        echo -e "   ${GREEN}✅ Batch endpoint working${NC}"
        
        # Check for totals object
        if echo "$RESPONSE" | grep -q '"totals"'; then
            echo -e "   ${GREEN}✅ 'totals' object found in response${NC}"
            echo ""
            echo "   Totals breakdown:"
            echo "$RESPONSE" | jq '.totals' 2>/dev/null || echo "$RESPONSE" | grep -A 5 '"totals"'
        else
            echo -e "   ${YELLOW}⚠️  'totals' object NOT found in response${NC}"
            echo "   This might be okay if migration hasn't run yet"
            echo ""
            echo "   Full response structure:"
            echo "$RESPONSE" | jq 'keys' 2>/dev/null || echo "$RESPONSE" | head -20
        fi
        
        # Check for counts
        if echo "$RESPONSE" | grep -q '"counts"'; then
            echo -e "   ${GREEN}✅ 'counts' object found${NC}"
        else
            echo -e "   ${RED}❌ 'counts' object missing${NC}"
        fi
    fi
else
    echo "   Skipping batch test (no batch ID provided)"
fi
echo ""

# Test 3: Invoice search endpoint
echo "3. Testing invoice search endpoint..."
SEARCH_RESPONSE=$(curl -s "$BASE_URL/api/invoices/search?q=test&limit=5")
if echo "$SEARCH_RESPONSE" | grep -q '"items"'; then
    echo -e "   ${GREEN}✅ Invoice search endpoint working${NC}"
    
    # Check for date range support
    DATE_SEARCH=$(curl -s "$BASE_URL/api/invoices/search?fromDate=2024-01-01&toDate=2024-12-31&limit=5")
    if echo "$DATE_SEARCH" | grep -q '"items"'; then
        echo -e "   ${GREEN}✅ Date range search working${NC}"
    else
        echo -e "   ${YELLOW}⚠️  Date range search may not be working${NC}"
    fi
else
    echo -e "   ${RED}❌ Invoice search endpoint error${NC}"
    echo "$SEARCH_RESPONSE"
fi
echo ""

# Test 4: Database migration check
echo "4. Database migration status..."
echo "   To check if migration has run, execute this SQL:"
echo ""
echo "   SELECT column_name FROM information_schema.columns"
echo "   WHERE table_name = 'reconciliation_batches'"
echo "   AND column_name IN ('confirmed_count', 'external_count');"
echo ""
echo "   Or run: ./run_migration.sh"
echo ""

# Test 5: Worker status (if we can check)
echo "5. Worker status..."
echo "   Check Render worker logs for:"
echo "   - 'Worker started'"
echo "   - 'Claimed job' or 'Processing job'"
echo "   - Any error messages"
echo ""

echo "=========================================="
echo "✅ Verification complete!"
echo ""
echo "Next steps:"
echo "1. Run migration if needed: ./run_migration.sh"
echo "2. Check worker logs in Render dashboard"
echo "3. Test upload a CSV file"
echo "4. Check browser console for frontend errors"

