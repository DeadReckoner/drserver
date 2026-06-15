# Start from a lightweight base image.
# Must be >= the `go` directive in go.mod (currently 1.25.0) — the builder runs
# with GOTOOLCHAIN=local, so it won't auto-download a newer toolchain.
FROM golang:1.25-alpine

# Set the working directory
WORKDIR /app

# Copy your Go module files to download dependencies
COPY go.mod go.sum ./

RUN go mod download

# Copy the rest of your source code
COPY . .

# Build the application
RUN go build -o main .

# Expose the port your server listens on
EXPOSE 8080

# Start the server
CMD ["./main"]
