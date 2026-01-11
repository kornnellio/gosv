# gosv - Go Process Supervisor

A minimal process supervisor written in Go, demonstrating Unix process management primitives.

## Features

- Process lifecycle management (start, stop, restart)
- Automatic restart with exponential backoff
- Zombie process reaping via SIGCHLD
- Graceful shutdown (SIGTERM → wait → SIGKILL)
- Process group management (kill entire process trees)
- Cgroup v2 resource limits (memory, CPU)
- Runtime introspection via /proc
- JSON configuration support

## Quick Start

```bash
# Build
go build -o gosv .

# Run a single command with supervision
./gosv --run "python3 -m http.server 8080"

# Run with config file
./gosv --config services.json

# Run demo (heartbeat + crasher processes)
./gosv
```

## Usage

```
gosv - Go Process Supervisor

Options:
  --config <file>    Load services from JSON config
  --run <command>    Run single command with supervision

Signals:
  SIGTERM, SIGINT    Graceful shutdown
  SIGUSR1            Dump process introspection info

Examples:
  gosv --run "node server.js"
  gosv --config production.json
```

## Configuration

```json
{
  "services": [
    {
      "name": "web",
      "command": "python3",
      "args": ["-m", "http.server", "8080"],
      "max_restarts": 5,
      "memory_mb": 256,
      "cpu_percent": 50
    },
    {
      "name": "worker",
      "command": "/usr/bin/node",
      "args": ["worker.js"],
      "max_restarts": 10
    }
  ]
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      SUPERVISOR                              │
│                                                              │
│  ┌─────────────┐    SIGCHLD     ┌─────────────────────────┐ │
│  │   Signal    │ ◄───────────── │        Kernel           │ │
│  │   Handler   │                └─────────────────────────┘ │
│  └──────┬──────┘                           ▲                │
│         │                                  │                │
│         ▼                                  │ exit           │
│  ┌─────────────┐                ┌──────────┴──────────┐     │
│  │    Reap     │                │                     │     │
│  │   Zombies   │                │  ┌───────────────┐  │     │
│  │  (wait4)    │                │  │  Process A    │  │     │
│  └──────┬──────┘                │  │  (PID 1234)   │  │     │
│         │                       │  └───────────────┘  │     │
│         ▼                       │                     │     │
│  ┌─────────────┐                │  ┌───────────────┐  │     │
│  │  Restart    │ ──restart───►  │  │  Process B    │  │     │
│  │  Handler    │                │  │  (PID 5678)   │  │     │
│  │  (backoff)  │                │  └───────────────┘  │     │
│  └─────────────┘                │                     │     │
│                                 │    Process Group    │     │
│                                 └─────────────────────┘     │
└─────────────────────────────────────────────────────────────┘
```

## Key Concepts

### Process Groups
Each managed process runs in its own process group. This allows killing the entire process tree:
```go
// Create process group on start
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

// Kill entire group
syscall.Kill(-pid, syscall.SIGTERM)  // Note: negative PID
```

### Zombie Reaping
When a child dies, the kernel sends SIGCHLD. We must call wait() to clean up:
```go
func reapZombies() {
    for {
        pid, _ := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
        if pid <= 0 {
            break  // No more zombies
        }
        // Update process state, trigger restart
    }
}
```

### Exponential Backoff
Prevents rapid restart loops:
```
Restart 1: wait 1s
Restart 2: wait 2s
Restart 3: wait 4s
Restart 4: wait 8s
...
After 60s stable: reset counter
```

### Graceful Shutdown
```
1. Send SIGTERM to all processes
2. Wait up to 10 seconds
3. Send SIGKILL to survivors
4. Reap all zombies
5. Exit
```

## Cgroups v2 Resource Limits

```bash
# Memory limit
echo "268435456" > /sys/fs/cgroup/gosv/myservice/memory.max

# CPU limit (50%)
echo "50000 100000" > /sys/fs/cgroup/gosv/myservice/cpu.max

# Add process to cgroup
echo $PID > /sys/fs/cgroup/gosv/myservice/cgroup.procs
```

## Introspection

Send SIGUSR1 to dump process info:
```bash
kill -USR1 $(pgrep gosv)
```

Output includes:
- Process state (running, stopped, failed)
- Memory usage (VmRSS from /proc/PID/status)
- Open file descriptors
- Memory maps

## Files

```
gosv/
├── main.go         # Entry point, CLI parsing
├── supervisor.go   # Main event loop, signal handling
├── process.go      # Process lifecycle management
├── cgroup.go       # Cgroup v2 resource limits
├── proc.go         # /proc filesystem introspection
└── server.py       # Test HTTP server
```

## Testing

```bash
# Start supervisor with demo processes
./gosv

# In another terminal, kill a process
pkill -f "heartbeat"
# Watch supervisor restart it

# Test graceful shutdown
kill $(pgrep gosv)

# Test introspection
kill -USR1 $(pgrep gosv)
```

## Syscalls Used

| Syscall | Purpose |
|---------|---------|
| fork/exec | Create child processes |
| setpgid | Create process groups |
| wait4 | Reap zombie processes |
| kill | Send signals to process groups |
| signal | Register signal handlers |

## License

Educational project - MIT License
