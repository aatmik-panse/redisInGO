# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.19-alpine AS build
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application
RUN CGO_ENABLED=0 go build -o /kvcache

# Final stage
FROM alpine:latest
WORKDIR /

# Install CA certificates for HTTPS connections
RUN apk --no-cache add ca-certificates

# Copy the binary from the build stage
COPY --from=build /kvcache /kvcache

# Create a non-root user and group
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Set ownership
RUN chown -R appuser:appgroup /kvcache

# Switch to non-root user
USER appuser

# Expose the port
EXPOSE 7171

# Run the application
ENTRYPOINT ["/kvcache"]
