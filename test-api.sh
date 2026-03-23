#!/bin/bash

HOST=${1:-localhost}
PORT=${2:-8080}
BASE_URL="http://$HOST:$PORT"

echo "Testing SBS Engine API at $BASE_URL"
echo "=================================="

# Test health/root endpoint
echo "1. Testing root endpoint..."
curl -s -o /dev/null -w "Status: %{http_code}\n" $BASE_URL/

# Test if server is responding
echo "2. Testing server response..."
curl -s $BASE_URL/ || echo "Connection failed"

# Test with Origin header
echo "3. Testing with Origin header..."
curl -H "Origin: http://localhost:8080" -s $BASE_URL/

# Check if port is open
echo "3. Checking if port $PORT is open..."
nc -zv $HOST $PORT 2>&1 | grep -q "succeeded" && echo "Port is open" || echo "Port is closed"

echo "=================================="