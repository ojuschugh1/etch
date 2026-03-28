Output demo.gif

Set Shell bash
Set FontSize 14
Set Width 900
Set Height 400
Set Theme "Dracula"
Set Padding 20
Set Framerate 30
Set PlaybackSpeed 0.8

Hide
Type "rm -rf /tmp/etch-demo && mkdir -p /tmp/etch-demo/snapshots"
Enter
Type "mkdir -p /tmp/etch-demo/api"
Enter
Type `echo '{"users":[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]}' > /tmp/etch-demo/api/users`
Enter
Type `echo '{"status":"healthy","version":"1.2.0"}' > /tmp/etch-demo/api/health`
Enter
Type "cd /tmp/etch-demo"
Enter
Type "python3 -m http.server 9000 --directory api &>/dev/null & echo $! > /tmp/api.pid 2>/dev/null"
Enter
Sleep 1s
Show

Type "clear"
Enter
Sleep 300ms

Type "# etch — detect API changes instantly"
Enter
Sleep 1.5s

Type "# step 1: record baseline responses"
Enter
Sleep 300ms
Type "etch record --port 8080 --snap-dir snapshots & echo $! > /tmp/etch.pid"
Enter
Sleep 2s
Type "curl -s -x http://localhost:8080 http://localhost:9000/users | jq ."
Enter
Sleep 1.5s
Type "curl -s -x http://localhost:8080 http://localhost:9000/health | jq ."
Enter
Sleep 1.5s
Type "kill $(cat /tmp/etch.pid) 2>/dev/null; sleep 0.5"
Enter
Sleep 1.5s

Type "# step 2: test — everything matches baseline"
Enter
Sleep 300ms
Type "etch test --port 8080 --snap-dir snapshots & echo $! > /tmp/etch.pid"
Enter
Sleep 2s
Type "curl -s -x http://localhost:8080 http://localhost:9000/users > /dev/null"
Enter
Sleep 1s
Type "curl -s -x http://localhost:8080 http://localhost:9000/health > /dev/null"
Enter
Sleep 1s
Type "kill $(cat /tmp/etch.pid) 2>/dev/null; sleep 0.5"
Enter
Sleep 3s

Type "# step 3: API changes silently — Alice removed, Charlie added"
Enter
Sleep 300ms
Type `echo '{"users":[{"id":2,"name":"Bob"},{"id":3,"name":"Charlie"}]}' > api/users`
Enter
Sleep 500ms
Type "etch test --port 8080 --snap-dir snapshots & echo $! > /tmp/etch.pid"
Enter
Sleep 2s
Type "curl -s -x http://localhost:8080 http://localhost:9000/users > /dev/null"
Enter
Sleep 1s
Type "kill $(cat /tmp/etch.pid) 2>/dev/null; sleep 0.5"
Enter
Sleep 5s

Type "# step 4: approve the intentional change"
Enter
Sleep 300ms
Type "etch approve --snap-dir snapshots"
Enter
Sleep 2s

Type "# step 5: re-test — clean"
Enter
Sleep 300ms
Type "etch test --port 8080 --snap-dir snapshots & echo $! > /tmp/etch.pid"
Enter
Sleep 2s
Type "curl -s -x http://localhost:8080 http://localhost:9000/users > /dev/null"
Enter
Sleep 1s
Type "kill $(cat /tmp/etch.pid) 2>/dev/null; sleep 0.5"
Enter
Sleep 4s

Hide
Type "kill $(cat /tmp/api.pid) 2>/dev/null"
Enter
Show
