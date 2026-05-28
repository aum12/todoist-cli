.PHONY: build test lint install clean

build:
	go build -o bin/todoist-aum ./cmd/todoist-aum

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/todoist-aum

clean:
	rm -rf bin/

build-mcp:
	go build -o bin/todoist-aum-mcp ./cmd/todoist-aum-mcp

install-mcp:
	go install ./cmd/todoist-aum-mcp

build-all: build build-mcp
