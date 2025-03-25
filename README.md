# Key-Value Cache Service in Go

## Overview
This project implements an in-memory key-value cache service with enhanced performance under heavy load by utilizing both sharding and consistent hashing. The consistent hash ring with virtual nodes ensures that key distribution remains stable even if the number of shards changes. The cache supports:
- **PUT**: Insert or update a key–value pair.
- **GET**: Retrieve the value associated with a given key.

The service is containerized using Docker and listens on port 7171.

## Architecture
- **Sharded Design**: Uses 64 shards (configurable) to distribute keys and improve concurrency
- **Consistent Hashing**: Uses xxHash algorithm for efficient key distribution 
- **Atomic Counters**: Thread-safe statistics using atomic operations
- **Read-Write Locks**: Per-shard locks to maximize concurrent access

## API Endpoints

### PUT Operation
- **HTTP Method**: POST
- **Endpoint**: `/put`
- **Request Body**:
  ```json
  {
    "key": "string (max 256 characters)",
    "value": "string (max 256 characters)"
  }
  ```
- **Response**:
  ```json
  {
    "status": "OK"
  }
  ```

### GET Operation
- **HTTP Method**: GET
- **Endpoint**: `/get?key=yourkey`
- **Response (found)**:
  ```json
  {
    "status": "OK",
    "key": "yourkey",
    "value": "yourvalue"
  }
  ```
- **Response (not found)**:
  ```json
  {
    "status": "ERROR",
    "message": "Key not found."
  }
  ```

## Installation

### Using Docker

```bash
# Build and run with Docker Compose
docker compose up --build

# Or build and run manually
docker build -t goredis:latest .
docker run -p 7171:7171 goredis:latest
```

The server will be available at http://localhost:7171.

## Load Testing

The project includes a load testing tool in the `test` directory for benchmarking performance.

```bash
# Navigate to the test directory
cd test

# Run the load test
go run test.go -n 50000 -c 200 -g 70
```

See [USAGE.md](USAGE.md) for more detailed instructions and load test parameters.

## Performance Considerations

- The cache uses sharding to distribute keys across multiple partitions
- Each shard has its own mutex to allow for concurrent operations on different shards
- xxHash provides fast and high-quality hashing for key distribution
- Atomic operations ensure thread-safe statistics collection


## For Docker Deployment

See [README.Docker.md](README.Docker.md) for detailed Docker deployment instructions.