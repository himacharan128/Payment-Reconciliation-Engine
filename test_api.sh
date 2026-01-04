#!/bin/bash

# Test script to verify API endpoints
# Usage: ./test_api.sh [base_url]
# Example: ./test_api.sh https://payment-reconciliation-api-2.onrender.com

BASE_URL="${1:-http://localhost:8080}"

echo "Testing API: $BASE_URL"
echo "================================"
echo ""

# Test 1: Upload a CSV (if you have one)
echo "1. Testing batch status endpoint..."
echo "   GET $BASE_URL/api/reconciliation/:batchId"
echo ""
echo "   Please provide a batch ID to test:"
read -p "   Batch ID: " BATCH_ID

if [ -z "$BATCH_ID" ]; then
    echo "   Skipping batch status test (no batch ID provided)"
else
    echo ""
    echo "   Response:"
    curl -s "$BASE_URL/api/reconciliation/$BATCH_ID" | jq '.' || curl -s "$BASE_URL/api/reconciliation/$BATCH_ID"
    echo ""
    echo ""
    
    # Check if totals exist
    echo "   Checking for 'totals' object..."
    RESPONSE=$(curl -s "$BASE_URL/api/reconciliation/$BATCH_ID")
    if echo "$RESPONSE" | grep -q '"totals"'; then
        echo "   ✅ 'totals' object found in response"
        echo "$RESPONSE" | jq '.totals' || echo "$RESPONSE" | grep -A 5 '"totals"'
    else
        echo "   ❌ 'totals' object NOT found in response"
        echo "   Full response:"
        echo "$RESPONSE"
    fi
fi

echo ""
echo "================================"
echo "Test complete!"

