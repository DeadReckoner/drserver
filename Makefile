
.PHONY: all tidy vet fmt build test clean start_server

all: tidy fmt lint build test clean
run: tidy fmt lint test start_server
ci: tidy fmt build test clean


tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	- golangci-lint run

build:
	go build .

start_server:
	go run .

test:
	go test -count=1 -cover ./...

clean:
	rm -f ./server
