#  Key-Value Cache Service

## Overview
This project implements an in-memory key-value cache service with enhanced performance under heavy load by utilizing both sharding and consistent hashing. The consistent hash ring with virtual nodes ensures that key distribution remains stable even if the number of shards changes. The cache supports:
- **PUT**: Insert or update a key–value pair.
- **GET**: Retrieve the value associated with a given key.

The service is containerized using Docker and listens on port 7171.

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