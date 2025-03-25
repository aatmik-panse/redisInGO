# Docker Deployment Guide for Redis-like KV Cache

## Local Development and Testing

When you're ready to start your application using Docker, run:
```bash
docker compose up --build
```

Your cache service will be available at http://localhost:7171.

You can test it with simple curl commands:

```bash
# Store a key-value pair
curl -X POST http://localhost:7171/put -H "Content-Type: application/json" -d '{"key": "key", "value": "value"}'

# Retrieve a value
curl http://localhost:7171/get?key=key
```