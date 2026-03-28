# Python + Etch

## Setup

```bash
pip install requests
```

## Record

```bash
# terminal 1
./etch record --port 8080

# terminal 2
http_proxy=http://localhost:8080 python examples/python/app.py
```

## Test

```bash
# terminal 1
./etch test --port 8080

# terminal 2
http_proxy=http://localhost:8080 python examples/python/app.py
```
