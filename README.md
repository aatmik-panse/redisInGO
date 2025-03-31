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

## SDK Usage

A Go SDK is available to interact with the cache service without dealing with the binary protocol directly.

### Installation

The SDK is part of the project and can be imported as:

```go
import "goredis/sdk"
```

### Basic Client Usage

```go
// Create a client with default options
client := sdk.NewClient("your-cache-hostname", nil)
defer client.Close()

// Store a value
err := client.Put("my-key", "my-value")
if err != nil {
    log.Fatalf("Failed to store: %v", err)
}

// Retrieve a value
value, err := client.Get("my-key")
if err != nil {
    if err == sdk.ErrKeyNotFound {
        fmt.Println("Key not found")
    } else {
        log.Fatalf("Error retrieving: %v", err)
    }
} else {
    fmt.Printf("Retrieved value: %s\n", value)
}
```

### Client Configuration

You can customize the client behavior with `ClientOptions`:

```go
options := &sdk.ClientOptions{
    Port:           7171,            // Server port
    PoolSize:       10,              // Connection pool size
    ConnectTimeout: 5 * time.Second, // Connection timeout
    ReadTimeout:    3 * time.Second, // Read operation timeout
    WriteTimeout:   3 * time.Second, // Write operation timeout
}

client := sdk.NewClient("your-cache-hostname", options)
```

### Thread Safety

The SDK client is thread-safe and includes connection pooling for efficient concurrent usage.

### Example Application

Run the example application to test the SDK:

```bash
cd sdk/examples
go run main.go -host your-cache-hostname
```

Parameters:
- `-host`: Hostname of the cache server (default: "localhost")
- `-port`: Port of the cache server (default: 7171)
- `-pool`: Connection pool size (default: 5)
- `-timeout`: Operation timeout (default: 5s)

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

You can test the binary protocol with these more reliable commands:

```bash
# Store a key-value pair (key1=value1)
# Format: 0x01 (PUT) + 0x0004 (key length) + 0x0006 (value length) + "key1" + "value1"
printf "\x01\x00\x04\x00\x06key1value1" | nc localhost 7171

# Retrieve a value for key1
# Format: 0x02 (GET) + 0x0004 (key length) + "key1"
printf "\x02\x00\x04key1" | nc localhost 7171
```

Alternatively, you can create these testing files:

```bash
# Create a file with PUT command
echo -ne '\x01\x00\x04\x00\x06key1value1' > put_command.bin

# Create a file with GET command
echo -ne '\x02\x00\x04key1' > get_command.bin

# Use the files to test
cat put_command.bin | nc localhost 7171
cat get_command.bin | nc localhost 7171
```

For easier testing, use the provided SDK and example application.

## Configuration

The service has the following configurable constants in the code:

- `maxLength`: Maximum length for keys and values (default: 256)
- `shardCount`: Number of shards for distributing keys (default: 512)
- `ttlSeconds`: Default TTL for cached items in seconds (default: 120)
- `maxMemoryPct`: Maximum memory usage percentage before eviction (default: 70)

## For More Information

- [USAGE.md](USAGE.md) - Detailed usage instructions
- [README.Docker.md](README.Docker.md) - Additional Docker deployment notes