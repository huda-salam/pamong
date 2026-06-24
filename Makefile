## Pamong Framework — Makefile
## Target tersedia: build, test, lint, run, migrate, tidy, vet

.PHONY: build test lint run migrate tidy vet

# Binary output
BIN ?= bin/pamong

## build: compile seluruh kode (termasuk semua package)
build:
	go build -o $(BIN) ./cmd/server/...
	@echo "build OK → $(BIN)"

## test: jalankan unit test dengan race detector
test:
	go test -race -count=1 ./...

## test-integration: jalankan unit + integration test (butuh Postgres)
test-integration:
	go test -race -count=1 -tags=integration ./...

## vet: jalankan go vet
vet:
	go vet ./...

## lint: jalankan go vet + pamongctl lint (custom analyzer Pamong)
lint: vet
	go run ./tools/pamongctl lint ./...

## tidy: bersihkan go.mod dan go.sum
tidy:
	go mod tidy

## run: jalankan server
run:
	go run ./cmd/server/...

## migrate: placeholder — implementasi di Phase 1.2.3
migrate:
	@echo "migration runner belum diimplementasi (Phase 1.2.3)"

## help: tampilkan target yang tersedia
help:
	@grep -E '^## ' Makefile | sed 's/## //'

# Pastikan direktori bin ada sebelum build
$(BIN): | bin
bin:
	mkdir -p bin
