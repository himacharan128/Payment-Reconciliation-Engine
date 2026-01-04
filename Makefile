.PHONY: db-up db-down db-logs db-ps api worker web seed api-bg worker-bg dev stop

db-up:
	docker compose up -d

db-down:
	docker compose down

db-logs:
	docker compose logs -f db

db-ps:
	docker compose ps

# Run API locally (expects env vars to be exported or loaded)
api:
	cd backend && go run ./cmd/api

# Run worker locally
worker:
	cd backend && go run ./cmd/worker

# Run API in background
api-bg:
	@mkdir -p logs
	@set -a && source .env.local && set +a && cd backend && (go run ./cmd/api > ../logs/api.log 2>&1 &) && sleep 1 && echo "API started in background (PID: $$!)"

# Run worker in background
worker-bg:
	@mkdir -p logs
	@set -a && source .env.local && set +a && cd backend && (go run ./cmd/worker > ../logs/worker.log 2>&1 &) && sleep 1 && echo "Worker started in background (PID: $$!)"

# Run both API and worker in background
dev:
	@mkdir -p logs
	@echo "Stopping any existing processes..."
	@pkill -f "go run ./cmd/api" 2>/dev/null || true
	@pkill -f "go run ./cmd/worker" 2>/dev/null || true
	@ps aux | grep -E "[g]o run ./cmd/api|[g]o run ./cmd/worker" | awk '{print $$2}' | xargs kill -9 2>/dev/null || true
	@sleep 1
	@echo "Starting API and Worker in background..."
	@set -a && source .env.local && set +a && cd backend && (go run ./cmd/api > ../logs/api.log 2>&1 &) && sleep 1 && (go run ./cmd/worker > ../logs/worker.log 2>&1 &) && sleep 1
	@echo "✓ API and Worker started in background"
	@echo "Check logs: tail -f logs/api.log logs/worker.log"
	@echo "Stop with: make stop"

# Stop background processes
stop:
	@pkill -f "go run ./cmd/api" 2>/dev/null || true
	@pkill -f "go run ./cmd/worker" 2>/dev/null || true
	@ps aux | grep -E "[g]o run ./cmd/api|[g]o run ./cmd/worker" | awk '{print $$2}' | xargs kill -9 2>/dev/null || true
	@echo "✓ Stopped API and Worker"

# Run frontend locally
web:
	cd frontend && npm run dev

# Seed invoices from CSV
seed:
	@set -a && source .env.local && set +a && cd backend && go run ./cmd/seed

