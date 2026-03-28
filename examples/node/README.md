# Node.js + Etch

## Record

```bash
# terminal 1
./etch record --port 8080

# terminal 2
http_proxy=http://localhost:8080 node examples/node/app.js
```

## Test

```bash
# terminal 1
./etch test --port 8080

# terminal 2
http_proxy=http://localhost:8080 node examples/node/app.js
```
