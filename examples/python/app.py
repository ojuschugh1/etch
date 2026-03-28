"""
Example: Using Etch with Python requests.

1. Start etch:    ./etch record --port 8080
2. Run this:      http_proxy=http://localhost:8080 python examples/python/app.py
3. Stop etch:     ctrl+c
4. Test later:    ./etch test --port 8080
                  http_proxy=http://localhost:8080 python examples/python/app.py
"""

import requests
import json

API_BASE = "http://httpbin.org"

def main():
    # simple GET
    resp = requests.get(f"{API_BASE}/get", params={"page": 1, "limit": 10})
    print(f"GET /get -> {resp.status_code}")
    print(json.dumps(resp.json(), indent=2)[:200])
    print()

    # POST with JSON body
    resp = requests.post(f"{API_BASE}/post", json={"name": "alice", "role": "admin"})
    print(f"POST /post -> {resp.status_code}")
    print(json.dumps(resp.json(), indent=2)[:200])
    print()

    # headers endpoint
    resp = requests.get(f"{API_BASE}/headers")
    print(f"GET /headers -> {resp.status_code}")
    print(json.dumps(resp.json(), indent=2)[:200])

if __name__ == "__main__":
    main()
