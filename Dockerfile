# Start from a lightweight base image
FROM golang:1.24-alpine

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
