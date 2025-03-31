# Key-Value Cache Service in Go

## Overview
This project implements an in-memory key-value cache service with enhanced performance under heavy load by utilizing both sharding and consistent hashing. The consistent hash ring with virtual nodes ensures that key distribution remains stable even if the number of shards changes. The cache supports:
- **PUT**: Insert or update a key–value pair.
- **GET**: Retrieve the value associated with a given key.

The service is containerized using Docker and listens on port 7171.

## Architecture
- **Sharded Design**: Uses 512 shards (configurable) to distribute keys and improve concurrency
- **Consistent Hashing**: Uses xxHash algorithm for efficient key distribution 
- **Memory Management**: Automatic eviction when memory usage exceeds threshold
- **LRU Eviction**: Least Recently Used items are evicted first when memory is constrained
- **Read-Write Locks**: Per-shard locks to maximize concurrent access

## Protocol

The service uses a binary protocol for communication:

### PUT Operation
- Command Byte: `0x01`
- Format: `0x01 + [2-byte key length] + [2-byte value length] + [key bytes] + [value bytes]`
- Response: `0x01` for success, `0x02 + [2-byte error message length] + [error message]` for failure

### GET Operation
- Command Byte: `0x02`
- Format: `0x02 + [2-byte key length] + [key bytes]`
- Success Response: `0x01 + [2-byte value length] + [value bytes]`
- Failure Response: `0x02 + [2-byte error message length] + [error message]`

## Docker Deployment

### Using Docker

```bash
# Build the Docker image
docker build -t goredis:latest .

# Run the container
docker run -p 7171:7171 goredis:latest
```

### Using Docker Compose

```bash
# Build and run with Docker Compose
docker compose up --build

# Run in detached mode
docker compose up -d

# Stop the service
docker compose down
```

The server will be available at port 7171 on your host machine.

## Testing Your Deployment

You can test the binary protocol with simple commands:

```bash
# Store a key-value pair (key1=value1)
echo -e '\x01\x00\x04\x00\x06key1value1' | nc localhost 7171

# Retrieve a value for key1
echo -e '\x02\x00\x04key1' | nc localhost 7171
```

## Configuration

The service has the following configurable constants in the code:

- `maxLength`: Maximum length for keys and values (default: 256)
- `shardCount`: Number of shards for distributing keys (default: 512)
- `ttlSeconds`: Default TTL for cached items in seconds (default: 120)
- `maxMemoryPct`: Maximum memory usage percentage before eviction (default: 70)


## For More Information

- [USAGE.md](USAGE.md) - Detailed usage instructions
- [README.Docker.md](README.Docker.md) - Additional Docker deployment notes