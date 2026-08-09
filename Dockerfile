# ---- build stage ------------------------------------------------------------
# Must be >= the `go` directive in go.mod (currently 1.25.0) — the builder runs
# with GOTOOLCHAIN=local, so it won't auto-download a newer toolchain.
FROM golang:1.25-alpine AS build

WORKDIR /src

# Dependencies first: this layer is cached until go.mod/go.sum actually change,
# so ordinary source edits skip the module download entirely.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a static binary with no libc dependency, which is what
# lets the runtime stage be this small.
# -trimpath strips local filesystem paths out of the binary; -s -w drop the
# symbol and DWARF tables, which are dead weight in a container.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/drserver .

# ---- runtime stage ----------------------------------------------------------
# The previous single-stage image shipped the full Go toolchain and the entire
# source tree to production. Nothing here but the binary and the certs it needs
# to make outbound TLS calls (Supabase, Anthropic/OpenAI, Expo, RevenueCat).
FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && adduser --disabled-password --gecos "" --uid 10001 app

COPY --from=build /out/drserver /usr/local/bin/drserver

# Drop root. Nothing in the app writes to the filesystem or binds a privileged
# port, so there is no reason to run as uid 0.
USER app

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/drserver"]
