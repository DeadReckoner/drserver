
.PHONY: all tidy fmt build test clean

all: tidy fmt lint build test clean


tidy:
	go mod tidy

fmt:
	go fmt ./...

lint:
	- golangci-lint run

build:
	go build .

test:
	go test -cover ./...

clean:
	rm ./server