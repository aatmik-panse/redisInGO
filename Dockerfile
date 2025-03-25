# Stage 1: Build
FROM golang:1.20-alpine AS builder
WORKDIR /app

# Copy the source code
COPY . .

# Build the binary with static linking
RUN CGO_ENABLED=0 go build -o kvcache .

# Stage 2: Final Image
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/kvcache .
EXPOSE 7171
CMD ["./kvcache"]