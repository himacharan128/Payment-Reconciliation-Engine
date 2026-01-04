.PHONY: db-up db-down db-logs db-ps api worker web seed

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

# Run frontend locally
web:
	cd frontend && npm run dev

# Seed invoices from CSV
seed:
	@set -a && source .env.local && set +a && cd backend && go run ./cmd/seed

