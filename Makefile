LOCAL_BIN := $(CURDIR)/bin
PROTOC_GEN_GO := $(LOCAL_BIN)/protoc-gen-go
PROTOC_GEN_GO_GRPC := $(LOCAL_BIN)/protoc-gen-go-grpc
MOCKGEN := $(LOCAL_BIN)/mockgen
GOOSE := $(LOCAL_BIN)/goose
SERVICE_NAME := content
DB_DSN ?= postgres://postgres:change-me@localhost:5432/content?sslmode=disable
COMPONENT_TEST_DSN ?= $(DB_DSN)
MIGRATION_NAME ?= content_change
PROTO_INCLUDE ?= /usr/include

.PHONY: help test ctest build db-status db-up db-down db-create db-seed mocks generate proto run env-up env-down tidy

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "%-12s %s\n", $$1, $$2}'

test: ## Run unit tests
	go test ./...

ctest: ## Run component tests against a running PostgreSQL
	COMPONENT_TEST_DSN="$(COMPONENT_TEST_DSN)" go test -tags=component ./tests/component/...

build: ## Build the service binary
	go build ./...

db-status: ## Show schema migration status (via parker)
	go run ./cmd/content migrate --dir migrations/schema --dsn "$(DB_DSN)" status

db-up: ## Apply schema migrations (via parker)
	go run ./cmd/content migrate --dir migrations/schema --dsn "$(DB_DSN)" up

db-down: ## Roll back the last schema migration (via parker)
	go run ./cmd/content migrate --dir migrations/schema --dsn "$(DB_DSN)" down

db-create: $(GOOSE) ## Create a new schema migration file: make db-create MIGRATION_NAME=add_table
	$(GOOSE) -dir migrations/schema create "$(MIGRATION_NAME)" sql

db-seed: $(GOOSE) ## Apply seed migrations
	$(GOOSE) -dir migrations/seeds postgres "$(DB_DSN)" up

mocks: $(MOCKGEN) ## Regenerate mocks from service contracts
	mkdir -p internal/mock
	$(MOCKGEN) -source=internal/pkg/service/contracts.go -destination=internal/mock/content_item_dependencies.go -package=mock

generate: proto mocks ## Regenerate proto and mocks

proto: $(PROTOC_GEN_GO) $(PROTOC_GEN_GO_GRPC) ## Regenerate protobuf files
	PATH="$(LOCAL_BIN):$$PATH" protoc -I . \
		-I "$(PROTO_INCLUDE)" \
		--go_out=. \
		--go_opt=module=github.com/overmindv/content \
		--go-grpc_out=. \
		--go-grpc_opt=module=github.com/overmindv/content \
		api/content/content.proto

run: ## Run the service locally
	go run ./cmd/content

env-up: ## Start local PostgreSQL and Kafka
	docker compose up -d postgres kafka

env-down: ## Stop local PostgreSQL and Kafka
	docker compose down -v

tidy: ## Tidy go modules
	go mod tidy

$(PROTOC_GEN_GO):
	GOBIN="$(LOCAL_BIN)" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10

$(PROTOC_GEN_GO_GRPC):
	GOBIN="$(LOCAL_BIN)" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

$(MOCKGEN):
	GOBIN="$(LOCAL_BIN)" go install go.uber.org/mock/mockgen@v0.6.0

$(GOOSE):
	GOBIN="$(LOCAL_BIN)" go install github.com/pressly/goose/v3/cmd/goose@v3.24.3
