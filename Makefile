.PHONY: up down logs run test

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

run: up
	cd go-api && export $$(grep -v '^#' ../.env | xargs) && go run ./cmd/api/main.go

test:
	cd go-api && go test -race -short ./...
