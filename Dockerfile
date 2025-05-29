# syntax=docker/dockerfile:1

# Build stage (based on Ubuntu)
FROM golang:1.19 AS build
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application (CGO disabled for static binary)
RUN CGO_ENABLED=0 go build -o /redis-go

# Final stage (runtime, based on Ubuntu)
FROM ubuntu:22.04
WORKDIR /

# Install CA certificates for HTTPS connections
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy the binary from the build stage
COPY --from=build /redis-go /redis-go

# Create a non-root user and group
RUN useradd -m -s /bin/bash appuser

# Set ownership
RUN chown -R appuser:appuser /redis-go

# Switch to non-root user
USER appuser

# Expose the port
EXPOSE 7171

# Run the application
ENTRYPOINT ["/redis-go"]
