# gosv - Go Process Supervisor

A Linux process supervisor written in Go for learning systems programming. Manages child processes with automatic restarts, resource limits via cgroups, and runtime introspection via `/proc`.

## Features

- **Process Lifecycle Management** - Start, monitor, and restart processes automatically
- **Zombie Reaping** - Proper handling of `SIGCHLD` with loop-based `wait4()` for coalesced signals
- **Signal Handling** - Graceful shutdown with `SIGTERM`/`SIGINT`, introspection with `SIGUSR1`
- **Process Groups** - Isolates process trees for clean signal propagation
- **Cgroups v2 Resource Limits** - Memory limits, CPU quotas, and PID limits
- **Systemd Integration** - Automatic delegation via `systemd-run` on managed systems
- **Runtime Introspection** - Dump process info from `/proc` (memory, file descriptors, memory maps)
- **Exponential Backoff** - Configurable restart delays with stability detection

## Linux Systems Programming Concepts

This project demonstrates:

| Concept | Implementation |
|---------|----------------|
| `fork`/`exec` | Process creation via `os/exec` |
| `wait4` with `WNOHANG` | Non-blocking zombie reaping |
| `setpgid` | Process group isolation |
| `kill(-pgid, sig)` | Signal entire process tree |
| `/proc` filesystem | Process introspection (`status`, `fd/*`, `maps`) |
| cgroups v2 | Resource limits (`memory.max`, `cpu.max`, `pids.max`) |
| Signal handling | Channel-based signal notification |

## Building

```bash
go build -o gosv .
```

Requires Go 1.23+.

## Usage

### Demo Mode (no arguments)

```bash
./gosv
```

Runs two demo processes:
- `heartbeat` - Prints a message every 2 seconds
- `crasher` - Crashes after 3 seconds to demonstrate restarts

### Single Command Mode

```bash
./gosv --run "python3 -m http.server 8080"
```

Supervises a single command with automatic restarts.

### Config File Mode

```bash
./gosv --config services.json
```

Manages multiple services defined in a JSON config file.

### Flags

| Flag | Description |
|------|-------------|
| `--config <file>` | Path to JSON config file |
| `--run "<command>"` | Run a single command |
| `--no-cgroup` | Disable cgroup resource limits |

## Configuration

Example `config.example.json`:

```json
{
  "services": [
    {
      "name": "webserver",
      "command": "python3",
      "args": ["-m", "http.server", "8080"],
      "max_restarts": 5,
      "memory_mb": 256,
      "cpu_percent": 50
    },
    {
      "name": "worker",
      "command": "/bin/sh",
      "args": ["-c", "while true; do echo 'processing...'; sleep 1; done"],
      "max_restarts": 3,
      "memory_mb": 128,
      "cpu_percent": 25
    }
  ]
}
```

### Service Options

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Service identifier |
| `command` | string | Executable path |
| `args` | []string | Command arguments |
| `max_restarts` | int | Maximum restart attempts (default: 3) |
| `memory_mb` | int | Memory limit in MB (requires cgroups) |
| `cpu_percent` | int | CPU quota as percentage (100 = 1 core) |

## Signals

| Signal | Action |
|--------|--------|
| `SIGTERM` / `SIGINT` | Graceful shutdown (SIGTERM to children, wait 10s, SIGKILL) |
| `SIGCHLD` | Reap zombie processes and trigger restart logic |
| `SIGUSR1` | Dump process introspection to stdout |
| `SIGHUP` | Reserved for config reload (not implemented) |

### Example: Introspection

```bash
# In terminal 1
./gosv --config services.json

# In terminal 2
kill -USR1 $(pgrep gosv)
```

Output:
```
=== Process: webserver ===
PID: 12345  Name: python3  State: S (sleeping)
PPID: 12340  Threads: 1
Memory: RSS=15432 KB  Virtual=45678 KB

Open file descriptors (5):
    0 -> /dev/null
    1 -> socket:[98765]
    2 -> socket:[98765]
    3 -> socket:[12345]
    4 -> /path/to/file

Memory maps (showing 10 of 42):
  55a1b2c3d000-55a1b2c3e000 r--p /usr/bin/python3
  ...
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Supervisor                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │  sigChan    │  │  reapChan   │  │ shutdownCh  │     │
│  │  (signals)  │  │  (restarts) │  │  (exit)     │     │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘     │
│         └────────────────┼────────────────┘             │
│                          ▼                              │
│                   ┌─────────────┐                       │
│                   │  Event Loop │                       │
│                   └──────┬──────┘                       │
│         ┌────────────────┼────────────────┐             │
│         ▼                ▼                ▼             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │  Process A  │  │  Process B  │  │  Process C  │     │
│  │  (pgid=A)   │  │  (pgid=B)   │  │  (pgid=C)   │     │
│  └─────────────┘  └─────────────┘  └─────────────┘     │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                    cgroups v2                           │
│  /sys/fs/cgroup/.../gosv/                               │
│  ├── supervisor/     (gosv process)                     │
│  ├── process_a/      memory.max, cpu.max, pids.max      │
│  ├── process_b/      memory.max, cpu.max, pids.max      │
│  └── process_c/      memory.max, cpu.max, pids.max      │
└─────────────────────────────────────────────────────────┘
```

## Key Implementation Details

### Zombie Reaping

When a child exits, it becomes a zombie until the parent calls `wait()`. Since `SIGCHLD` signals can be coalesced (multiple children die, one signal), we loop until no more zombies:

```go
for {
    pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
    if pid <= 0 || err != nil {
        break // No more zombies
    }
    // Handle exit...
}
```

### Process Groups

Each child gets its own process group (`Setpgid: true`). This allows killing the entire tree with `kill(-pgid, signal)`, ensuring no orphaned grandchildren.

### Cgroups v2 on Systemd

On systemd systems, `/sys/fs/cgroup` is managed by systemd. gosv handles this by:

1. Requesting a delegated scope via `systemd-run --user --scope -p Delegate=yes`
2. Moving itself to a leaf cgroup (`supervisor/`)
3. Enabling controllers in the now-empty parent
4. Creating per-service cgroups with resource limits

This respects the cgroup v2 "no internal processes" rule.

### Stability Detection

If a process runs for 60+ seconds before crashing, its restart counter resets. This prevents a long-running service from being marked as "failed" after occasional crashes.

## Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, CLI parsing, config loading |
| `supervisor.go` | Event loop, signal handling, restart logic |
| `process.go` | Process lifecycle (start, signal, state) |
| `proc.go` | `/proc` filesystem introspection |
| `cgroup.go` | Cgroups v2 resource limits |
| `zombie_demo.go` | Standalone demo of zombie processes |

## Testing

```bash
# Demo mode
./gosv

# Kill a process, watch it restart
kill -9 $(pgrep -f heartbeat)

# Introspection
kill -USR1 $(pgrep gosv)

# Graceful shutdown
kill -TERM $(pgrep gosv)

# With resource limits
./gosv --config config.example.json
cat /sys/fs/cgroup/.../webserver/memory.max  # Should show limit
```

## References

- [man 5 proc](https://man7.org/linux/man-pages/man5/proc.5.html) - `/proc` filesystem
- [man 7 cgroups](https://man7.org/linux/man-pages/man7/cgroups.7.html) - Control groups
- [man 2 wait4](https://man7.org/linux/man-pages/man2/wait4.2.html) - Process waiting
- [man 2 kill](https://man7.org/linux/man-pages/man2/kill.2.html) - Signal sending
- [man 7 signal](https://man7.org/linux/man-pages/man7/signal.7.html) - Signal handling

## License

Educational project - use freely.
