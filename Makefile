
.PHONY: all tidy vet fmt build test integration_test clean

all: tidy fmt lint build test clean
local: tidy fmt lint test run
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

run:
	go run .


# Unit tests only. The integration suite lives behind the `integration` build
# tag, so `./...` (without -tags) skips it — keep it that way; do not add
# -tags integration here. Run the integration suite with `make integration_test`.
test:
	go test -cover ./...

# Integration tests against a throwaway Postgres via testcontainers. Requires a
# running Docker daemon. Gated behind the `integration` build tag so it never
# runs as part of `make test` / `make ci`.
integration_test:
	go test -tags integration -count=1 ./test/integration/...

clean:
	rm -f ./server
