# gosv Command Line Cheatsheet

## Building

```bash
# Build the supervisor
/home/me/go/bin/go build -o gosv .

# Run zombie demo
/home/me/go/bin/go run zombie_demo.go
```

## Running gosv

```bash
# Demo mode (two test processes)
./gosv

# Supervise a single command
./gosv -run "python3 server.py"
./gosv -run "python3 -m http.server 8080"

# Run with config file
./gosv -config config.example.json

# Run in background
./gosv -run "python3 server.py" &
GOSV_PID=$!
```

---

## Signal Tests

### Signals to gosv

```bash
# Graceful shutdown
kill $(pgrep -x gosv)
kill -TERM $(pgrep -x gosv)

# Interrupt (same as Ctrl+C)
kill -INT $(pgrep -x gosv)

# Dump process introspection (/proc info)
kill -USR1 $(pgrep -x gosv)

# Reload config (not implemented)
kill -HUP $(pgrep -x gosv)
```

### Signals to Supervised Child

```bash
# Graceful kill → triggers restart
kill $(pgrep -x python3)
kill -TERM $(pgrep -x python3)
kill -15 $(pgrep -x python3)

# Hard kill → triggers restart
kill -9 $(pgrep -x python3)
kill -KILL $(pgrep -x python3)

# Simulate segfault → triggers restart
kill -SEGV $(pgrep -x python3)
kill -11 $(pgrep -x python3)

# Pause process (stops execution)
kill -STOP $(pgrep -x python3)

# Resume paused process
kill -CONT $(pgrep -x python3)

# Kill child of gosv specifically (avoids killing gosv)
kill $(pgrep -P $(pgrep -x gosv))
```

---

## Testing Scenarios

### Test 1: Basic Start/Stop

```bash
./gosv -run "python3 server.py" &
GOSV_PID=$!
sleep 2
curl -s http://localhost:8080 | head -1
kill $GOSV_PID
wait $GOSV_PID 2>/dev/null
echo "PASS"
```

### Test 2: Restart on Kill

```bash
# Terminal 1
./gosv -run "python3 server.py"

# Terminal 2
kill $(pgrep -P $(pgrep -x gosv))
# Watch Terminal 1 - should show restart
curl http://localhost:8080   # should work after restart
```

### Test 3: Graceful Shutdown (No Orphans)

```bash
./gosv -run "python3 server.py" &
GOSV_PID=$!
sleep 2
kill $GOSV_PID
wait $GOSV_PID 2>/dev/null
sleep 1
pgrep -f "server.py" && echo "ORPHAN - BAD" || echo "No orphan - GOOD"
```

### Test 4: Kill with Different Signals

```bash
# Terminal 1
./gosv -run "python3 server.py"

# Terminal 2 - try each, watch restart
kill -TERM $(pgrep -x python3)   # exit code 0 (handled)
kill -KILL $(pgrep -x python3)   # exit code 137 (128+9)
kill -SEGV $(pgrep -x python3)   # exit code 139 (128+11)
```

### Test 5: Exhaust All Restarts

```bash
# Start gosv
./gosv -run "python3 server.py"

# In another terminal - kill until gosv gives up
for i in {1..15}; do
  echo "Kill attempt $i"
  pkill -x python3
  sleep 20   # wait longer than backoff
done

# Watch for "attempt 10/10" then no more restarts
```

### Test 6: Stability Reset

```bash
# Terminal 1
./gosv -run "python3 server.py"

# Terminal 2 - kill a few times
pkill -x python3; sleep 5
pkill -x python3; sleep 5
pkill -x python3; sleep 5
# Now at attempt 3/10

# Wait 60+ seconds for stability reset
sleep 70

# Kill again - should show "resetting restart counter"
pkill -x python3
# Watch for: "[gosv] stable for 1m0s, resetting"
# Should say "attempt 1/10" not "attempt 4/10"
```

### Test 7: /proc Introspection

```bash
# Terminal 1
./gosv -run "python3 server.py"

# Terminal 2
kill -USR1 $(pgrep -x gosv)
# Watch Terminal 1 - shows memory maps, FDs, process state
```

### Test 8: Port Conflict Recovery

```bash
# Start something on port 8080 first
python3 -m http.server 8080 &
OLD_PID=$!

# Start gosv - it will fail to bind, retry
./gosv -run "python3 server.py"
# Watch server.py retry binding

# Kill the old server
kill $OLD_PID
# Watch gosv's server.py succeed
```

### Test 9: Multiple Services

```bash
# Create config
cat > /tmp/multi.json << 'EOF'
{
  "services": [
    {"name": "web", "command": "python3", "args": ["server.py"]},
    {"name": "worker", "command": "sh", "args": ["-c", "while true; do echo working; sleep 5; done"]}
  ]
}
EOF

# Run with config
./gosv -config /tmp/multi.json

# Kill one service - other should continue
pkill -x python3
# "worker" keeps running, "web" restarts
```

### Test 10: Crash Simulations

```bash
# Process that always crashes
./gosv -run "sh -c 'echo starting; sleep 2; exit 1'"

# Process that crashes randomly
./gosv -run "sh -c 'sleep \$((RANDOM % 5)); exit 1'"

# Process that segfaults itself
./gosv -run "sh -c 'kill -SEGV \$\$'"
```

### Test 11: Zombie Demonstration

```bash
# Run the standalone zombie demo
/home/me/go/bin/go run zombie_demo.go

# Check for zombies on system
ps aux | grep Z
```

### Test 12: Rapid Start/Stop

```bash
for i in {1..10}; do
  ./gosv -run "sleep 30" &
  sleep 1
  kill $!
  wait $! 2>/dev/null
done

# Check for leftover processes
pgrep -a sleep && echo "LEAKED" || echo "CLEAN"
```

---

## Verification Commands

### Process Inspection

```bash
# Find gosv
pgrep -x gosv
pgrep -a gosv

# Find python
pgrep -x python3
pgrep -f server.py
pgrep -a -f "server.py"

# Find children of gosv
pgrep -P $(pgrep -x gosv)

# Show all python processes
ps aux | grep python3 | grep -v grep

# Check for zombies
ps aux | grep Z

# Detailed process info
ps -o pid,ppid,pgid,state,comm -p <pid>
```

### Port Verification

```bash
# Check what's on port 8080
ss -tlnp | grep 8080
lsof -i :8080
netstat -tlnp | grep 8080

# Verify port is free
ss -tlnp | grep 8080 || echo "Port free"
```

### Server Verification

```bash
# Test if server responds
curl http://localhost:8080
curl -s http://localhost:8080 | head -3

# Test with timeout
curl --max-time 2 http://localhost:8080
```

### Orphan Check

```bash
# Check for orphan python
pgrep -x python3 && echo "ORPHAN EXISTS" || echo "No orphan"

# Check for orphan server.py specifically
pgrep -f "server.py" && echo "ORPHAN EXISTS" || echo "No orphan"
```

---

## Cleanup Commands

```bash
# Kill gosv gracefully
kill $(pgrep -x gosv)

# Kill all gosv processes
pkill -f gosv

# Kill all server.py
pkill -f "server.py"

# Kill all python3
pkill -9 -f python3

# Free port 8080
fuser -k 8080/tcp

# Nuclear cleanup
pkill -9 -f "gosv"
pkill -9 -f "server.py"
pkill -9 -f "python3"
fuser -k 8080/tcp
sleep 1
echo "Cleaned"
```

---

## /proc Exploration

```bash
# Read process status (state, memory, threads)
cat /proc/<pid>/status

# Read memory maps (code, heap, libraries)
cat /proc/<pid>/maps

# List open file descriptors
ls -la /proc/<pid>/fd/

# Read command line
cat /proc/<pid>/cmdline | tr '\0' ' '

# See executable path
readlink /proc/<pid>/exe

# See current working directory
readlink /proc/<pid>/cwd

# See environment variables
cat /proc/<pid>/environ | tr '\0' '\n'
```

---

## cgroups (requires root)

```bash
# Create cgroup
sudo mkdir /sys/fs/cgroup/gosv

# Enable controllers
echo "+cpu +memory +pids" | sudo tee /sys/fs/cgroup/cgroup.subtree_control

# Set memory limit (256MB)
echo 268435456 | sudo tee /sys/fs/cgroup/gosv/memory.max

# Set CPU limit (50%)
echo "50000 100000" | sudo tee /sys/fs/cgroup/gosv/cpu.max

# Set max processes
echo 100 | sudo tee /sys/fs/cgroup/gosv/pids.max

# Move process to cgroup
echo <pid> | sudo tee /sys/fs/cgroup/gosv/cgroup.procs

# Check current memory usage
cat /sys/fs/cgroup/gosv/memory.current

# Remove cgroup (must be empty)
sudo rmdir /sys/fs/cgroup/gosv
```

---

## Full Test Script

```bash
#!/bin/bash
cd /home/me/Desktop/Go_Linux/gosv

echo "=== Test 1: Basic start/stop ==="
./gosv -run "python3 server.py" &
PID=$!
sleep 2
curl -s localhost:8080 | head -1 && echo "Server OK"
kill $PID
wait $PID 2>/dev/null
echo "PASS"
echo ""

echo "=== Test 2: Restart on kill ==="
./gosv -run "python3 server.py" &
PID=$!
sleep 2
pkill -x python3
sleep 5
curl -s localhost:8080 | head -1 && echo "PASS - Restarted" || echo "FAIL"
kill $PID
wait $PID 2>/dev/null
echo ""

echo "=== Test 3: Graceful shutdown (no orphan) ==="
./gosv -run "python3 server.py" &
PID=$!
sleep 2
kill -TERM $PID
wait $PID 2>/dev/null
sleep 1
pgrep -f "server.py" && echo "FAIL - orphan" || echo "PASS - clean"
echo ""

echo "=== Test 4: Introspection ==="
./gosv -run "python3 server.py" &
PID=$!
sleep 2
kill -USR1 $PID
sleep 1
kill $PID
wait $PID 2>/dev/null
echo "PASS - check output above"
echo ""

echo "=== All tests complete ==="
```

---

## Reference Tables

### Signal Reference

| Signal  | Number | Catchable | Meaning                    |
|---------|--------|-----------|----------------------------|
| SIGTERM | 15     | Yes       | Graceful termination       |
| SIGKILL | 9      | No        | Forced death               |
| SIGINT  | 2      | Yes       | Interrupt (Ctrl+C)         |
| SIGCHLD | 17     | Yes       | Child state changed        |
| SIGHUP  | 1      | Yes       | Hangup / reload config     |
| SIGUSR1 | 10     | Yes       | User-defined               |
| SIGUSR2 | 12     | Yes       | User-defined               |
| SIGSTOP | 19     | No        | Pause process              |
| SIGCONT | 18     | Yes       | Resume process             |
| SIGSEGV | 11     | Yes       | Segmentation fault         |

### Exit Code Reference

| Code    | Meaning                          |
|---------|----------------------------------|
| 0       | Success                          |
| 1       | General error                    |
| 2       | Misuse of command                |
| 126     | Permission denied                |
| 127     | Command not found                |
| 128+N   | Killed by signal N               |
| 137     | Killed by SIGKILL (128+9)        |
| 139     | Killed by SIGSEGV (128+11)       |
| 143     | Killed by SIGTERM (128+15)       |

### Backoff Example

| Attempt | Delay (2s base, 1.5x factor) |
|---------|------------------------------|
| 1       | 2.0s                         |
| 2       | 3.0s                         |
| 3       | 4.5s                         |
| 4       | 6.75s                        |
| 5       | 10.1s                        |
| 6       | 15.2s                        |
| 7       | 22.8s                        |
| 8       | 34.2s                        |

### Process States

| State | Meaning                              |
|-------|--------------------------------------|
| R     | Running or runnable                  |
| S     | Sleeping (interruptible)             |
| D     | Disk sleep (uninterruptible)         |
| Z     | Zombie (dead, not reaped)            |
| T     | Stopped (by signal or debugger)      |
| X     | Dead                                 |
