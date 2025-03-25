# Usage Instructions for Redis-like KV Cache

## Server (Main Application)

### Running with Docker

1. Build the Docker image:
```bash
docker build -t goredis:latest .
```

2. Run the server:
```bash
docker run -p 7171:7171 goredis:latest
```

3. Access the service at http://localhost:7171

### Running Directly

```bash
go build -o kvcache
./kvcache
```

## Load Test Tool

The load test is in a separate module in the `test` directory.

### Running the Load Test

Option 1: Build and run in one step:
```bash
# Navigate to the test directory
cd test

# Run the Go file directly without building first
go run main.go
```

Option 2: Build then run (creates an executable):
```bash
# Navigate to the test directory
cd test

# Build the load tester
go build -o loadtest

# Run the load test executable
./loadtest
```

### Troubleshooting the Load Test

If you're having issues with Go version compatibility (e.g., with Go 1.24.1 in go.mod):

```bash
# Navigate to the test directory
cd test

# Update the go.mod file to match your Go version
# Edit go.mod and change "go 1.24.1" to match your installed version, e.g., "go 1.20"

# Then run directly
go run main.go
```

### Load Test Parameters

- `-host`: Host of the cache server (default: "localhost")
- `-port`: Port of the cache server (default: 7171)
- `-n`: Total number of requests to make (default: 100000)
- `-c`: Number of concurrent workers (default: 100)
- `-g`: Percentage of GET requests (default: 80)
- `-klen`: Length of keys to generate (default: 10)
- `-vlen`: Length of values to generate (default: 100)
- `-random-len`: Randomize key/value lengths (default: false)

## Complete Example Workflow

1. Start the server in one terminal:
```bash
# From the main project directory:
go build -o kvcache
./kvcache
```

2. Run a load test in another terminal:
```bash
cd test
go run main.go -n 50000 -c 200 -g 70
```

This will run 50,000 requests with 200 concurrent workers, where 70% are GET requests and 30% are PUT requests.
