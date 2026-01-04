#!/bin/bash
set -a
source .env.local
set +a

echo "Testing upload endpoint..."
echo "Uploading bank_transactions.csv..."

response=$(curl -s -X POST \
  -F "file=@seed/data/bank_transactions.csv" \
  http://localhost:8080/api/reconciliation/upload)

echo "Response: $response"

# Extract batchId if present
batchId=$(echo $response | grep -o '"batchId":"[^"]*"' | cut -d'"' -f4)

if [ ! -z "$batchId" ]; then
  echo ""
  echo "✅ Upload successful!"
  echo "Batch ID: $batchId"
  echo ""
  echo "Verifying file exists..."
  if [ -f "data/uploads/${batchId}.csv" ]; then
    echo "✅ File saved at: data/uploads/${batchId}.csv"
  else
    echo "❌ File not found"
  fi
  echo ""
  echo "Checking database..."
  docker compose exec -T db psql -U app -d app -c "SELECT id, filename, status FROM reconciliation_batches WHERE id = '${batchId}';"
  docker compose exec -T db psql -U app -d app -c "SELECT batch_id, file_path, status FROM reconciliation_jobs WHERE batch_id = '${batchId}';"
else
  echo "❌ Upload failed"
  echo "$response"
fi
