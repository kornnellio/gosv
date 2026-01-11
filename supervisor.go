package main

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Supervisor manages multiple processes
type Supervisor struct {
	processes map[string]*Process
	mu        sync.RWMutex

	// Channels for event handling
	sigChan    chan os.Signal
	reapChan   chan struct{}
	shutdownCh chan struct{}

	wg sync.WaitGroup
}

// NewSupervisor creates a supervisor ready to manage processes
func NewSupervisor() *Supervisor {
	return &Supervisor{
		processes:  make(map[string]*Process),
		sigChan:    make(chan os.Signal, 10),
		reapChan:   make(chan struct{}, 10),
		shutdownCh: make(chan struct{}),
	}
}

// AddProcess registers a process to be supervised
func (s *Supervisor) AddProcess(p *Process) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processes[p.Name] = p
}

// setupSignals configures signal handling
//
// KEY CONCEPT: Signal handling in Go
// Go's runtime already handles some signals (SIGURG for preemption).
// We use signal.Notify to receive signals on a channel rather than
// using raw sigaction. This plays nice with Go's scheduler.
func (s *Supervisor) setupSignals() {
	// SIGCHLD: Child process state changed (exited, stopped, continued)
	// This is THE signal that tells us to call wait() and reap zombies
	signal.Notify(s.sigChan, syscall.SIGCHLD)

	// SIGTERM: Graceful termination request
	// We'll propagate this to children before exiting
	signal.Notify(s.sigChan, syscall.SIGTERM)

	// SIGINT: Interrupt (Ctrl+C)
	signal.Notify(s.sigChan, syscall.SIGINT)

	// SIGHUP: Traditionally means "reload config"
	// We could use this to restart processes or reload config
	signal.Notify(s.sigChan, syscall.SIGHUP)

	// SIGUSR1: User-defined signal - we use it to dump process info
	signal.Notify(s.sigChan, syscall.SIGUSR1)
}

// reapZombies handles SIGCHLD by calling wait() on all children
//
// KEY CONCEPT: Zombie processes
// When a child exits, it becomes a "zombie" - its exit status is held
// by the kernel until the parent calls wait(). If we don't reap:
// 1. Process table entries leak
// 2. PIDs aren't recycled
// 3. `ps` shows processes in Z state
//
// Since SIGCHLD can be coalesced (multiple children die, one signal),
// we must loop until wait() returns no more children.
func (s *Supervisor) reapZombies() {
	for {
		// Wait for ANY child, non-blocking
		var wstatus syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &wstatus, syscall.WNOHANG, nil)

		if pid <= 0 || err != nil {
			// No more zombies to reap
			break
		}

		// Find which of our processes this was
		s.mu.RLock()
		var found *Process
		for _, p := range s.processes {
			if p.pid == pid {
				found = p
				break
			}
		}
		s.mu.RUnlock()

		if found != nil {
			found.mu.Lock()
			found.state = StateStopped
			if wstatus.Exited() {
				found.exitCode = wstatus.ExitStatus()
			} else if wstatus.Signaled() {
				found.exitCode = 128 + int(wstatus.Signal())
			}
			// Record how long process ran before dying (for stability check)
			found.lastUptime = time.Since(found.startTime)
			fmt.Printf("[gosv] process %s (pid=%d) exited with code %d\n",
				found.Name, pid, found.exitCode)
			// Zero the PID to prevent stale PID issues
			found.pid = 0
			found.mu.Unlock()

			// Trigger restart evaluation
			s.reapChan <- struct{}{}
		} else {
			// Unknown child - could be grandchild if we're init
			fmt.Printf("[gosv] reaped unknown pid %d\n", pid)
		}
	}
}

// StableAfter is how long a process must run before we consider it "stable"
// and reset the restart counter. This prevents a long-running service from
// being permanently marked as "exhausted" after a few crashes.
const StableAfter = 60 * time.Second

// handleRestarts checks for dead processes and restarts them
func (s *Supervisor) handleRestarts() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.processes {
		p.mu.Lock()

		// If process ran long enough before dying, it was stable - reset counter
		// We check lastUptime (how long it ran) not time.Since(startTime)
		if p.lastUptime > StableAfter && p.restarts > 0 {
			fmt.Printf("[gosv] %s was stable for %v before exit, resetting restart counter\n",
				p.Name, p.lastUptime)
			p.restarts = 0
		}

		shouldRestart := p.state == StateStopped &&
			p.restarts < p.MaxRestarts

		if shouldRestart {
			p.restarts++
			delay := time.Duration(float64(p.RestartDelay) *
				math.Pow(p.BackoffFactor, float64(p.restarts-1)))

			fmt.Printf("[gosv] restarting %s in %v (attempt %d/%d)\n",
				p.Name, delay, p.restarts, p.MaxRestarts)

			p.mu.Unlock()

			// Restart after delay
			go func(proc *Process, d time.Duration) {
				time.Sleep(d)
				if err := proc.Start(); err != nil {
					fmt.Printf("[gosv] restart failed: %v\n", err)
				}
			}(p, delay)
		} else {
			p.mu.Unlock()
		}
	}
}

// gracefulShutdown stops all processes with SIGTERM, then SIGKILL
func (s *Supervisor) gracefulShutdown() {
	fmt.Println("[gosv] initiating graceful shutdown...")

	s.mu.RLock()
	procs := make([]*Process, 0, len(s.processes))
	for _, p := range s.processes {
		procs = append(procs, p)
	}
	s.mu.RUnlock()

	// Phase 1: SIGTERM to all
	for _, p := range procs {
		p.mu.Lock()
		state := p.state
		p.mu.Unlock()
		if state == StateRunning {
			fmt.Printf("[gosv] sending SIGTERM to %s\n", p.Name)
			p.Signal(syscall.SIGTERM)
		}
	}

	// Wait up to 10 seconds for graceful exit
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			// Phase 2: SIGKILL stragglers
			for _, p := range procs {
				p.mu.Lock()
				pid := p.pid
				p.mu.Unlock()
				if pid != 0 {
					fmt.Printf("[gosv] sending SIGKILL to %s\n", p.Name)
					p.Signal(syscall.SIGKILL)
				}
			}
			// Final reap
			s.reapZombies()
			return
		case <-ticker.C:
			// Reap any dead children to update state
			s.reapZombies()

			allDead := true
			for _, p := range procs {
				// Check if process is actually alive using kill(pid, 0)
				if p.pid != 0 {
					err := syscall.Kill(p.pid, 0)
					if err == nil {
						// Process still exists
						allDead = false
					}
				}
			}
			if allDead {
				fmt.Println("[gosv] all processes terminated gracefully")
				return
			}
		}
	}
}

// Run starts all processes and enters the supervisor loop
func (s *Supervisor) Run() error {
	s.setupSignals()

	// Start all registered processes
	s.mu.RLock()
	for _, p := range s.processes {
		if err := p.Start(); err != nil {
			s.mu.RUnlock()
			return err
		}
	}
	s.mu.RUnlock()

	fmt.Println("[gosv] supervisor running, press Ctrl+C to stop")

	// Main supervisor loop
	for {
		select {
		case sig := <-s.sigChan:
			switch sig {
			case syscall.SIGCHLD:
				// Child state changed - reap zombies
				s.reapZombies()

			case syscall.SIGTERM, syscall.SIGINT:
				// Shutdown requested
				s.gracefulShutdown()
				return nil

			case syscall.SIGHUP:
				// Could reload config here
				fmt.Println("[gosv] received SIGHUP (reload not implemented)")

			case syscall.SIGUSR1:
				// Dump process introspection
				fmt.Println("[gosv] received SIGUSR1 - dumping process info")
				s.Introspect()
			}

		case <-s.reapChan:
			// A child was reaped - check if we need to restart
			s.handleRestarts()

		case <-s.shutdownCh:
			s.gracefulShutdown()
			return nil
		}
	}
}
