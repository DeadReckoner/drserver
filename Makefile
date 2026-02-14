
.PHONY: all tidy vet fmt build test clean

all: tidy fmt lint build test clean
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

test:
	go test -cover ./...

clean:
	rm -f ./server