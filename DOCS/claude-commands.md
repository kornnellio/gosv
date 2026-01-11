# Commands Used by Claude During This Session

## Project Setup

```bash
# Create project directory
mkdir -p /home/me/Desktop/Go_Linux/gosv/cmd/gosv

# Download and install Go
cd /tmp && curl -sLO https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
tar -C /home/me -xzf go1.23.4.linux-amd64.tar.gz

# Initialize Go module
/home/me/go/bin/go mod init github.com/gosv
```

## Building

```bash
# Build the supervisor
/home/me/go/bin/go build -o gosv .

# Run standalone demo
/home/me/go/bin/go run zombie_demo.go
```

## Running and Testing gosv

```bash
# Run in demo mode
./gosv

# Run with timeout for demo
timeout 15 ./gosv 2>&1 || true

# Run supervising a command
./gosv -run "python3 -m http.server 8080"
./gosv -run "python3 server.py"

# Run in background
./gosv -run "python3 server.py" &
GOSV_PID=$!

# Run with config
./gosv -config config.example.json
```

## Process Discovery

```bash
# Find gosv process
pgrep -x gosv
pgrep -a gosv

# Find python processes
pgrep -x python3
pgrep -f server.py
pgrep -a -f "server.py"
pgrep -a python3

# Find children of a process
pgrep -P $GOSV_PID
pgrep -P $(pgrep -x gosv)

# Show all processes matching pattern
ps aux | grep python3 | grep -v grep
ps aux | grep -E "postgres|docker" | grep -v grep | head -10
```

## Sending Signals

```bash
# Send SIGUSR1 to dump /proc info
kill -USR1 $(pgrep -x gosv)

# Kill python child (triggers restart)
kill $(pgrep -x python3)
kill $(pgrep -P $(pgrep -x gosv))
kill $PYTHON_PID

# Kill gosv (graceful shutdown)
kill $GOSV_PID

# Force kill
kill -9 53784
```

## Port Management

```bash
# Check what's on a port
ss -tlnp | grep 8080
ss -tlnp | grep 5432
lsof -i :8080
lsof -i :5432

# Kill whatever is on a port
fuser -k 8080/tcp

# Verify port is free
ss -tlnp | grep 8080 || echo "Port free"
ss -tlnp | grep 8080 || echo "Port 8080 is free"
```

## Process Cleanup

```bash
# Kill by pattern
pkill -f gosv
pkill -f "server.py"
pkill -f "http.server"
pkill -9 -f python3
pkill -9 -f "gosv"

# Kill specific PID
kill 111966
kill 51408
kill -9 114849
kill -9 53784

# Wait for background process
wait $GOSV_PID 2>/dev/null
```

## File Operations (via tools, not bash)

```bash
# Files created with Write tool:
/home/me/Desktop/Go_Linux/gosv/process.go
/home/me/Desktop/Go_Linux/gosv/supervisor.go
/home/me/Desktop/Go_Linux/gosv/cgroup.go
/home/me/Desktop/Go_Linux/gosv/proc.go
/home/me/Desktop/Go_Linux/gosv/main.go
/home/me/Desktop/Go_Linux/gosv/zombie_demo.go
/home/me/Desktop/Go_Linux/gosv/server.py
/home/me/Desktop/Go_Linux/gosv/config.example.json
/home/me/Desktop/Go_Linux/gosv/cmdline.md
/home/me/Desktop/Go_Linux/gosv/port-investigation.md

# Files edited with Edit tool:
/home/me/Desktop/Go_Linux/gosv/main.go        # Added exec prefix, changed restart delay
/home/me/Desktop/Go_Linux/gosv/supervisor.go  # Added SIGUSR1 handler, stability reset, fixed shutdown
/home/me/Desktop/Go_Linux/gosv/server.py      # Added SO_REUSEPORT and retry logic

# Files read with Read tool:
/home/me/Desktop/Go_Linux/gosv/main.go
/home/me/Desktop/Go_Linux/gosv/supervisor.go
/tmp/claude/-home-me-Desktop-Go-Linux/tasks/*.output  # Background task outputs
```

## Testing HTTP Server

```bash
# Basic curl
curl http://localhost:8080
curl -s http://localhost:8080 | head -3
curl -s http://localhost:8080 | head -5
curl -s http://localhost:8080 | head -1
```

## Reading Background Task Output

```bash
# Read task output files
cat /tmp/claude/-home-me-Desktop-Go-Linux/tasks/bc58e9c.output
cat /tmp/gosv.log
cat /tmp/gosv_test.log

# Grep log for specific patterns
grep -E "(started|exited|restarting)" /tmp/gosv.log
grep -E "\[server\]|\[gosv\]" /tmp/gosv_test.log
grep -E "\[server\]|\[gosv\]" /tmp/gosv_test.log | tail -10
```

## Docker Commands

```bash
# List containers with port info
docker ps --format "table {{.ID}}\t{{.Image}}\t{{.Ports}}" | grep 5432

# Stop container
docker stop 80dbc9b219f6
```

## System Service Management

```bash
# Check systemd units
systemctl list-units | grep -i postgres

# Stop/disable service (attempted)
sudo systemctl stop postgresql
sudo systemctl disable postgresql
```

## Project Structure Check

```bash
# List Go files
find . -name "*.go" -o -name "*.json" | head -20
```

## Full Test Scripts Run

```bash
# Complete test sequence
cd /home/me/Desktop/Go_Linux/gosv
./gosv -run "python3 server.py" &
GOSV_PID=$!
sleep 2
echo "Gosv PID: $GOSV_PID"
PYTHON_PID=$(pgrep -P $GOSV_PID -f python3)
echo "Python PID: $PYTHON_PID"
curl -s http://localhost:8080 | head -3
kill $PYTHON_PID
sleep 4
grep -E "\[server\]|\[gosv\]" /tmp/gosv.log | tail -10
curl -s http://localhost:8080 | head -3
kill $GOSV_PID
wait $GOSV_PID 2>/dev/null
```

## Verification Commands

```bash
# Check for orphans
pgrep -x python3 && echo "ORPHAN EXISTS - BAD" || echo "No orphan - GOOD"
pgrep -f "server.py" && echo "ORPHAN - BAD" || echo "No orphan - GOOD"

# Check for zombies
ps aux | grep Z

# Verify clean state
pgrep -a -f "gosv|server.py" || echo "All clean"
```

## Practice Example

```bash
# Start server for user to investigate
python3 -m http.server 9999 &
echo "Server started on port 9999. PID: $!"
```

---

## Summary by Category

| Category | Commands Used |
|----------|---------------|
| Setup | mkdir, curl, tar, go mod init |
| Build | go build, go run |
| Run | ./gosv, timeout |
| Find processes | pgrep, ps aux, grep |
| Signals | kill, kill -USR1, kill -9 |
| Ports | ss -tlnp, lsof -i, fuser -k |
| Cleanup | pkill, kill, wait |
| HTTP test | curl |
| Docker | docker ps, docker stop |
| Systemd | systemctl |
| Files | find, cat, grep |

---

## Tools Used (Claude Code internals)

| Tool | Purpose |
|------|---------|
| Bash | Execute shell commands |
| Write | Create new files |
| Edit | Modify existing files |
| Read | View file contents |
| Glob | Find files by pattern |
| Grep | Search file contents |
| TodoWrite | Track task progress |
