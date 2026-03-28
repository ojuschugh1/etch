#!/bin/bash
#
# Example: Using Etch with curl.
#
# 1. Start etch:    ./etch record --port 8080
# 2. Run this:      bash examples/curl/run.sh
# 3. Stop etch:     ctrl+c
# 4. Test later:    ./etch test --port 8080
#                   bash examples/curl/run.sh

export http_proxy=http://localhost:8080

echo "=== GET /get ==="
curl -s http://httpbin.org/get | head -20
echo

echo "=== POST /post ==="
curl -s -X POST http://httpbin.org/post \
  -H "Content-Type: application/json" \
  -d '{"name":"alice","role":"admin"}' | head -20
echo

echo "=== GET /ip ==="
curl -s http://httpbin.org/ip
echo
