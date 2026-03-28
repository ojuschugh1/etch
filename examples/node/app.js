/**
 * Example: Using Etch with Node.js fetch (undici).
 *
 * 1. Start etch:    ./etch record --port 8080
 * 2. Run this:      http_proxy=http://localhost:8080 node examples/node/app.js
 * 3. Stop etch:     ctrl+c
 * 4. Test later:    ./etch test --port 8080
 *                   http_proxy=http://localhost:8080 node examples/node/app.js
 */

const API_BASE = "http://httpbin.org";

async function main() {
  // simple GET
  let resp = await fetch(`${API_BASE}/get?page=1&limit=10`);
  let data = await resp.json();
  console.log(`GET /get -> ${resp.status}`);
  console.log(JSON.stringify(data, null, 2).slice(0, 200));
  console.log();

  // POST with JSON body
  resp = await fetch(`${API_BASE}/post`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: "alice", role: "admin" }),
  });
  data = await resp.json();
  console.log(`POST /post -> ${resp.status}`);
  console.log(JSON.stringify(data, null, 2).slice(0, 200));
}

main().catch(console.error);
