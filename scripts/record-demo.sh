#!/bin/bash
#
# Demo script for recording an asciinema GIF.
#
# Prerequisites:
#   brew install asciinema
#   pip install asciinema-agg   (for GIF conversion)
#
# Usage:
#   asciinema rec demo.cast -c "bash scripts/record-demo.sh"
#   agg demo.cast demo.gif
#
# Then add to README:
#   ![demo](demo.gif)

set -e

echo "$ ./etch record --port 9999"
./etch record --port 9999 &
PROXY_PID=$!
sleep 1

echo ""
echo "$ http_proxy=http://localhost:9999 curl -s http://httpbin.org/get"
http_proxy=http://localhost:9999 curl -s http://httpbin.org/get | head -10
echo "..."
sleep 1

echo ""
echo "$ http_proxy=http://localhost:9999 curl -s http://httpbin.org/ip"
http_proxy=http://localhost:9999 curl -s http://httpbin.org/ip
sleep 1

# stop recording
kill $PROXY_PID 2>/dev/null
wait $PROXY_PID 2>/dev/null
sleep 1

echo ""
echo "--- Now testing against stored snapshots ---"
echo ""

echo "$ ./etch test --port 9999"
./etch test --port 9999 &
PROXY_PID=$!
sleep 1

echo ""
echo "$ http_proxy=http://localhost:9999 curl -s http://httpbin.org/get > /dev/null"
http_proxy=http://localhost:9999 curl -s http://httpbin.org/get > /dev/null
sleep 1

echo ""
echo "$ http_proxy=http://localhost:9999 curl -s http://httpbin.org/ip > /dev/null"
http_proxy=http://localhost:9999 curl -s http://httpbin.org/ip > /dev/null
sleep 1

kill $PROXY_PID 2>/dev/null
wait $PROXY_PID 2>/dev/null
sleep 1

echo ""
echo "$ ./etch diff"
./etch diff
sleep 2

echo ""
echo "$ ./etch approve"
./etch approve
sleep 1

echo ""
echo "Done!"
