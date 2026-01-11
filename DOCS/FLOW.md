# gosv Codebase Overview

```
gosv/
├── main.go         Entry point, CLI parsing, config loading
├── supervisor.go   Main loop, signal handling, restart logic
├── process.go      Process lifecycle, spawning, reaping
├── cgroup.go       Resource limits via cgroups v2
├── proc.go         /proc filesystem introspection
├── server.py       Test HTTP server
└── zombie_demo.go  Standalone zombie demonstration
```

---

## Program Flow

```
main.go                          supervisor.go                    process.go
────────                         ──────────────                   ───────────
    │
    ▼
Parse flags
(-run, -config)
    │
    ▼
Create Supervisor ──────────────► NewSupervisor()
    │                                  │
    ▼                                  ▼
Create Process ◄─────────────────────── creates:
structs                                 - processes map
    │                                   - signal channel
    ▼                                   - reap channel
sup.AddProcess(p) ──────────────► stores in map
    │
    ▼
sup.Run() ──────────────────────► setupSignals()
                                       │
                                       ▼
                                  signal.Notify(SIGCHLD, SIGTERM, SIGUSR1...)
                                       │
                                       ▼
                                  Start all processes ────────► p.Start()
                                       │                            │
                                       ▼                            ▼
                                  Main loop:                   fork + exec
                                  for {                        set process group
                                    select {                   return PID
                                      case SIGCHLD:
                                        reapZombies() ─────────► wait4(-1, WNOHANG)
                                            │                    update state
                                            ▼
                                        reapChan <- signal

                                      case <-reapChan:
                                        handleRestarts() ──────► check stability
                                            │                    reset counter?
                                            ▼                    calculate backoff
                                        p.Start() ◄─────────────── restart

                                      case SIGTERM/SIGINT:
                                        gracefulShutdown() ────► SIGTERM to all
                                            │                    wait/reap
                                            ▼                    SIGKILL stragglers
                                        return

                                      case SIGUSR1:
                                        Introspect() ──────────► read /proc/[pid]/*
                                    }
                                  }
```

---

## File by File

### `main.go` — Entry Point

```go
func main() {
    // 1. Parse CLI flags
    configPath := flag.String("config", "", "...")
    singleCmd := flag.String("run", "", "...")

    // 2. Create supervisor
    sup := NewSupervisor()

    // 3. Add processes (from config or -run flag)
    if *configPath != "" {
        loadConfig(sup, *configPath)
    } else if *singleCmd != "" {
        p := &Process{
            Command: "/bin/sh",
            Args: []string{"-c", "exec " + *singleCmd},  // exec prevents orphans
        }
        sup.AddProcess(p)
    }

    // 4. Run forever
    sup.Run()
}
```

---

### `supervisor.go` — The Brain

```go
type Supervisor struct {
    processes  map[string]*Process   // managed processes
    sigChan    chan os.Signal        // receives signals
    reapChan   chan struct{}         // triggers restart check
}

func (s *Supervisor) Run() {
    s.setupSignals()      // register for SIGCHLD, SIGTERM, etc.

    // Start all processes
    for _, p := range s.processes {
        p.Start()
    }

    // Event loop
    for {
        select {
        case sig := <-s.sigChan:
            switch sig {
            case SIGCHLD:
                s.reapZombies()     // collect dead children
            case SIGTERM:
                s.gracefulShutdown()
                return
            case SIGUSR1:
                s.Introspect()      // dump /proc info
            }
        case <-s.reapChan:
            s.handleRestarts()      // restart dead processes
        }
    }
}
```

**Key functions:**

| Function | Purpose |
|----------|---------|
| `setupSignals()` | Register signal handlers |
| `reapZombies()` | Call wait4() to collect dead children |
| `handleRestarts()` | Check if process should restart, apply backoff |
| `gracefulShutdown()` | SIGTERM → wait → SIGKILL |
| `Introspect()` | Read /proc for each process |

---

### `process.go` — Process Lifecycle

```go
type Process struct {
    Name    string
    Command string
    Args    []string

    pid       int           // current PID
    state     ProcessState  // running, stopped, failed
    restarts  int           // restart counter
    startTime time.Time     // for stability check
}

func (p *Process) Start() error {
    p.cmd = exec.Command(p.Command, p.Args...)

    // KEY: Create own process group
    p.cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,
        Pgid: 0,
    }

    p.cmd.Start()
    p.pid = p.cmd.Process.Pid
    p.state = StateRunning
    p.startTime = time.Now()
}

func (p *Process) Signal(sig syscall.Signal) error {
    // Signal entire process group (negative PID)
    return syscall.Kill(-p.pid, sig)
}
```

---

### `cgroup.go` — Resource Limits

```go
type Cgroup struct {
    path string  // /sys/fs/cgroup/gosv/<name>
}

func (c *Cgroup) SetMemoryLimit(bytes int64) error {
    // Write to /sys/fs/cgroup/gosv/<name>/memory.max
    return os.WriteFile(path.Join(c.path, "memory.max"), ...)
}

func (c *Cgroup) SetCPUQuota(percent int) error {
    // Write "quota period" to cpu.max
    // 50% = "50000 100000"
}

func (c *Cgroup) AddProcess(pid int) error {
    // Write PID to cgroup.procs
}
```

---

### `proc.go` — Introspection

```go
type ProcInfo struct {
    PID        int
    Name       string
    State      string
    VmRSS      int64      // memory usage
    FDs        []FDInfo   // open files
    MemoryMaps []MemoryMap
}

func ReadProcInfo(pid int) (*ProcInfo, error) {
    // Read /proc/[pid]/status
    // Read /proc/[pid]/fd/*
    // Read /proc/[pid]/maps
}
```

---

## Signal Flow

```
                    KERNEL
                       │
                       │ SIGCHLD (child died)
                       ▼
┌─────────────────────────────────────────┐
│               SUPERVISOR                 │
│                                         │
│  sigChan ◄─── signal.Notify()           │
│     │                                   │
│     ▼                                   │
│  reapZombies()                          │
│     │                                   │
│     ├──► wait4(-1, WNOHANG)             │
│     │         │                         │
│     │         ▼                         │
│     │    Kernel returns exit status     │
│     │    Zombie deleted                 │
│     │                                   │
│     └──► reapChan <- signal             │
│               │                         │
│               ▼                         │
│         handleRestarts()                │
│               │                         │
│               ▼                         │
│         p.Start() ──► new child         │
└─────────────────────────────────────────┘
```

---

## Restart Logic

```go
func (s *Supervisor) handleRestarts() {
    for _, p := range s.processes {

        // 1. Stability reset (ran 60s+? reset counter)
        if time.Since(p.startTime) > 60*time.Second {
            p.restarts = 0
        }

        // 2. Should we restart?
        if p.state == StateStopped && p.restarts < p.MaxRestarts {

            // 3. Calculate backoff delay
            p.restarts++
            delay := RestartDelay * (BackoffFactor ^ restarts)

            // 4. Restart after delay
            go func() {
                time.Sleep(delay)
                p.Start()
            }()
        }
    }
}
```

---

## Key Syscalls Used

| Syscall | Where | Purpose |
|---------|-------|---------|
| `fork+exec` | process.go:Start() | Create child process |
| `setpgid` | process.go:Start() | Create process group |
| `wait4` | supervisor.go:reapZombies() | Collect dead children |
| `kill` | process.go:Signal() | Send signals to group |
| `signal.Notify` | supervisor.go:setupSignals() | Register signal handlers |

---

## Data Flow Summary

```
Config/CLI ──► Process structs ──► Supervisor.processes map
                                          │
                                          ▼
                                    Start all (fork+exec)
                                          │
                                          ▼
                                    Main event loop
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    ▼                     ▼                     ▼
               SIGCHLD              SIGTERM/INT              SIGUSR1
                    │                     │                     │
                    ▼                     ▼                     ▼
            reapZombies()         gracefulShutdown()      Introspect()
                    │                     │                     │
                    ▼                     ▼                     ▼
            handleRestarts()      SIGTERM→wait→SIGKILL    read /proc
                    │                     │
                    ▼                     ▼
               p.Start()              return (exit)
```

---

## The Unix Philosophy in gosv

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Child     │         │   Kernel    │         │   Parent    │
│             │         │             │         │  (gosv)     │
│  Lives      │         │  Witnesses  │         │  Retrieves  │
│  Dies       │────────►│  Records    │────────►│  Cleans up  │
│  (no duty)  │         │  Notifies   │         │  (at leisure)│
└─────────────┘         └─────────────┘         └─────────────┘

- Decoupled: Child doesn't need to notify parent
- Managed death: Kernel holds zombie until parent reaps
- Rich info: Exit code, signal, core dump available via wait()
```
