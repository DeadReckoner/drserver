

all: tidy fmt build test clean


tidy:
	go mod tidy

fmt:
	go fmt ./...

build:
	go build .

test:
	go test -cover ./...

clean:
	rm ./server