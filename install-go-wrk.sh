#!/bin/bash

echo "Installing go-wrk load testing tool..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed. Please install Go first."
    exit 1
fi

# Install go-wrk
go install github.com/tsliwowicz/go-wrk@latest

# Check if installation was successful
if [ $? -eq 0 ]; then
    echo "go-wrk installed successfully!"
    echo "You can find it at: $(go env GOPATH)/bin/go-wrk"
    echo "You may need to add this to your PATH:"
    echo "export PATH=\$PATH:\$(go env GOPATH)/bin"
    
    # Add to PATH for current session
    export PATH=$PATH:$(go env GOPATH)/bin
    
    echo "Path updated for current session. Try running: go-wrk -help"
else
    echo "Failed to install go-wrk. Please check your Go installation."
fi
