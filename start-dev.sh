#!/bin/bash
# Start both API and Worker in background

set -a
source .env.local
set +a

mkdir -p logs

echo "Starting API..."
cd backend
go run ./cmd/api > ../logs/api.log 2>&1 &
API_PID=$!
echo "API started (PID: $API_PID)"

echo "Starting Worker..."
go run ./cmd/worker > ../logs/worker.log 2>&1 &
WORKER_PID=$!
echo "Worker started (PID: $WORKER_PID)"

cd ..

echo ""
echo "✓ Both services started!"
echo "API PID: $API_PID"
echo "Worker PID: $WORKER_PID"
echo ""
echo "Check logs:"
echo "  tail -f logs/api.log"
echo "  tail -f logs/worker.log"
echo ""
echo "Stop with: pkill -f 'go run ./cmd/api' && pkill -f 'go run ./cmd/worker'"
echo "Or use: make stop"

