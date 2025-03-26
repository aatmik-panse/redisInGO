#!/bin/bash

# Make sure go-wrk is in your PATH
export PATH=$PATH:$(go env GOPATH)/bin

# Check if go-wrk is installed
if ! command -v go-wrk &> /dev/null; then
    echo "Error: go-wrk is not installed. Please run ./install-go-wrk.sh first."
    exit 1
fi

# Check if the server is running
if ! curl -s http://localhost:7171 &> /dev/null; then
    echo "Error: Server doesn't seem to be running at http://localhost:7171"
    echo "Please start the server first with: go run main.go"
    exit 1
fi

# Generate some test data first
echo "Preparing cache with test data..."
for i in {1..10}; do
    curl -s -X POST http://localhost:7171/put \
         -H "Content-Type: application/json" \
         -d "{\"key\":\"test-key-$i\",\"value\":\"test-value-$i\"}" > /dev/null
done

# Run GET tests
echo "Running GET load tests..."
go-wrk -c 1000 -d 10 "http://localhost:7171/get?key=test-key-1"

# Run POST tests with a temporary JSON file
echo "Running PUT load tests..."
echo '{"key":"test-load-key","value":"test-load-value"}' > /tmp/put_payload.json
go-wrk -c 1000 -d 10 -M POST -body /tmp/put_payload.json -H "Content-Type: application/json" http://localhost:7171/put
rm /tmp/put_payload.json

echo "Load tests completed!"
