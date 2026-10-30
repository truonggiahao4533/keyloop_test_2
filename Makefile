DATABASE_URL ?= postgres://inventory:inventory@localhost:5433/inventory?sslmode=disable
JWT_SECRET   ?= dev-secret-change-me-please
API_URL      ?= http://localhost:8080
ROLE         ?= manager
SUB          ?= demo-$(ROLE)

export DATABASE_URL JWT_SECRET

.PHONY: run test lint migrate-up sqlc seed seed-load smoke token

run:
	go run ./cmd/api

test:
	go test -race ./...

lint:
	go vet ./...
	golangci-lint run ./...

migrate-up:
	go run ./cmd/api migrate

sqlc:
	sqlc generate

seed:
	go run ./scripts/seed -reset

seed-load:
	go run ./scripts/seed -reset -n 250000

smoke:
	API_URL=$(API_URL) ./scripts/smoke.sh

token:
	@go run ./cmd/api token -role $(ROLE) -sub $(SUB)
