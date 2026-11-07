.PHONY: run run-worker test fmt

run:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

test:
	go test ./...

fmt:
	gofmt -w .
