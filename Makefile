.PHONY: test test-race lint vet build run fmt

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	gofmt -l cmd internal
	go vet ./...

build:
	go build -o bin/sleepyrouter ./cmd/sleepyrouter

run:
	go run ./cmd/sleepyrouter serve
