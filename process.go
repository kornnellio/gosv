package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ProcessState tracks the lifecycle of a managed process
type ProcessState int

const (
	StateStopped ProcessState = iota
	StateStarting
	StateRunning
	StateFailed
)

func (s ProcessState) String() string {
	return [...]string{"stopped", "starting", "running", "failed"}[s]
}

// Process represents a supervised process
type Process struct {
	Name    string
	Command string
	Args    []string

	// Runtime state
	cmd        *exec.Cmd
	pid        int
	state      ProcessState
	exitCode   int
	startTime  time.Time
	lastUptime time.Duration // How long process ran before last exit
	restarts   int

	// Restart policy
	MaxRestarts   int
	RestartDelay  time.Duration
	BackoffFactor float64

	// Resource limits (cgroup)
	MemoryLimit int64 // bytes
	CPUQuota    int   // percentage (100 = 1 core)

	// Cgroup for this process (nil if cgroups unavailable)
	cgroup *Cgroup

	mu sync.Mutex
}

// Start spawns the process with proper isolation
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.cmd = exec.Command(p.Command, p.Args...)
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr

	// KEY CONCEPT: SysProcAttr controls how the kernel creates the child
	p.cmd.SysProcAttr = &syscall.SysProcAttr{
		// Setpgid: Create new process group with child as leader
		// This is critical for signal propagation - we can kill the
		// entire group with kill(-pgid, signal)
		Setpgid: true,

		// Pgid: 0 means use child's PID as the PGID
		// If we set Pgid to a specific value, child joins that group
		Pgid: 0,

		// Foreground: false - don't make this the foreground process group
		// of controlling terminal (we're a supervisor, not a shell)
	}

	if err := p.cmd.Start(); err != nil {
		p.state = StateFailed
		return fmt.Errorf("failed to start %s: %w", p.Name, err)
	}

	p.pid = p.cmd.Process.Pid
	p.state = StateRunning
	p.startTime = time.Now()

	// Apply cgroup resource limits if configured
	if p.MemoryLimit > 0 || p.CPUQuota > 0 {
		cg, err := NewCgroup(p.Name)
		if err != nil {
			fmt.Printf("[gosv] warning: failed to create cgroup for %s: %v\n", p.Name, err)
		} else {
			p.cgroup = cg
			if p.MemoryLimit > 0 {
				if err := cg.SetMemoryLimit(p.MemoryLimit); err != nil {
					fmt.Printf("[gosv] warning: failed to set memory limit for %s: %v\n", p.Name, err)
				}
			}
			if p.CPUQuota > 0 {
				if err := cg.SetCPUQuota(p.CPUQuota); err != nil {
					fmt.Printf("[gosv] warning: failed to set CPU quota for %s: %v\n", p.Name, err)
				}
			}
			if err := cg.AddProcess(p.pid); err != nil {
				fmt.Printf("[gosv] warning: failed to add %s to cgroup: %v\n", p.Name, err)
			} else {
				fmt.Printf("[gosv] applied cgroup limits to %s (mem=%dMB, cpu=%d%%)\n",
					p.Name, p.MemoryLimit/(1024*1024), p.CPUQuota)
			}
		}
	}

	fmt.Printf("[gosv] started %s (pid=%d, pgid=%d)\n", p.Name, p.pid, p.pid)
	return nil
}

// Signal sends a signal to the process group
func (p *Process) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pid == 0 {
		return fmt.Errorf("process not running")
	}

	// KEY CONCEPT: Negative PID means signal the entire process group
	// This ensures children of children also receive the signal
	// Compare: kill(pid, sig) vs kill(-pgid, sig)
	pgid := -p.pid
	return syscall.Kill(pgid, sig)
}

// Wait blocks until process exits, returns exit code
func (p *Process) Wait() (int, error) {
	if p.cmd == nil || p.cmd.Process == nil {
		return -1, fmt.Errorf("process not started")
	}

	err := p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	p.state = StateStopped

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.exitCode = exitErr.ExitCode()
			return p.exitCode, nil
		}
		return -1, err
	}

	p.exitCode = 0
	return 0, nil
}

