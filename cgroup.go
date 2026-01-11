package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Cgroup manages a cgroup v2 for resource limits
//
// KEY CONCEPT: cgroups v2 unified hierarchy
// Unlike v1 (separate trees for cpu, memory, etc), v2 has ONE tree.
// Location: /sys/fs/cgroup/
// Each subdirectory is a cgroup. Controllers are enabled via cgroup.subtree_control
//
// Files we care about:
//   cgroup.procs        - PIDs in this cgroup (write PID to move process here)
//   memory.max          - Memory limit in bytes
//   memory.current      - Current memory usage
//   cpu.max             - CPU bandwidth limit "quota period" (e.g., "50000 100000" = 50%)
//   cpu.stat            - CPU usage statistics
//   pids.max            - Maximum number of processes
//
// SYSTEMD INTEGRATION:
// On systemd systems, the root cgroup is managed by systemd. We cannot create
// cgroups directly under /sys/fs/cgroup. Instead, we:
// 1. Find our current cgroup (from /proc/self/cgroup)
// 2. Create a "gosv" subcgroup there
// 3. Enable controllers and create per-process cgroups
type Cgroup struct {
	name string
	path string
}

const cgroupRoot = "/sys/fs/cgroup"

var (
	// baseCgroupPath is where we create our cgroups
	// Set by EnsureControllers() based on system configuration
	baseCgroupPath string
)

// getSelfCgroup returns the cgroup path of the current process
// Reads from /proc/self/cgroup which has format "0::/path/to/cgroup"
func getSelfCgroup() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}

	// Format for cgroup v2: "0::/user.slice/user-1000.slice/..."
	line := strings.TrimSpace(string(data))
	parts := strings.SplitN(line, "::", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected cgroup format: %s", line)
	}

	return parts[1], nil
}

// hasCgroupDelegation checks if the current cgroup has delegation enabled
// by testing if we can create a child cgroup and enable controllers
func hasCgroupDelegation() bool {
	selfCgroup, err := getSelfCgroup()
	if err != nil {
		return false
	}

	// Try to create test cgroup
	testPath := filepath.Join(cgroupRoot, selfCgroup, ".gosv-test")
	if err := os.Mkdir(testPath, 0755); err != nil {
		return false
	}
	defer os.Remove(testPath)

	// Check if subtree_control exists and is writable in parent
	parentPath := filepath.Join(cgroupRoot, selfCgroup)
	controlPath := filepath.Join(parentPath, "cgroup.subtree_control")

	// Try enabling a controller
	if err := os.WriteFile(controlPath, []byte("+memory"), 0644); err != nil {
		return false
	}

	return true
}

// RunWithDelegation re-executes the current process with systemd-run for cgroup delegation
// Returns true if re-exec happened (caller should exit), false if not needed or failed
func RunWithDelegation() bool {
	// Check if we already have delegation
	if hasCgroupDelegation() {
		return false
	}

	// Check if systemd-run is available
	systemdRun, err := exec.LookPath("systemd-run")
	if err != nil {
		fmt.Println("[gosv] systemd-run not found, continuing without cgroup delegation")
		return false
	}

	// Check if we're already in a delegated scope (avoid infinite loop)
	if os.Getenv("GOSV_DELEGATED") == "1" {
		fmt.Println("[gosv] already in delegated scope but delegation failed")
		return false
	}

	fmt.Println("[gosv] requesting cgroup delegation via systemd-run...")

	// Build command to re-exec ourselves
	args := []string{
		"--user",           // User scope
		"--scope",          // Transient scope (not service)
		"-p", "Delegate=yes", // Enable delegation
		"--",
	}
	args = append(args, os.Args...)

	cmd := exec.Command(systemdRun, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "GOSV_DELEGATED=1")

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Printf("[gosv] systemd-run failed: %v\n", err)
		return false
	}

	os.Exit(0)
	return true // Never reached
}

// findWritableCgroupBase finds a cgroup path where we can create children
// Tries in order:
// 1. Current process's cgroup (for systemd user sessions with delegation)
// 2. /sys/fs/cgroup directly (for root or non-systemd systems)
//
// KEY CONCEPT: cgroup v2 "no internal processes" rule
// A cgroup cannot have both processes AND children with controllers.
// To enable controllers for children, we must first move all processes
// from the parent to a leaf cgroup.
func findWritableCgroupBase() (string, error) {
	// Try 1: Use our current cgroup (works with systemd delegation)
	selfCgroup, err := getSelfCgroup()
	if err == nil && selfCgroup != "" {
		parentPath := filepath.Join(cgroupRoot, selfCgroup)

		// KEY CONCEPT: To enable controllers in subtree_control, the cgroup
		// must have no processes. We need to:
		// 1. Create a "supervisor" leaf cgroup for ourselves
		// 2. Move ourselves there
		// 3. Enable controllers in the parent
		// 4. Create per-service cgroups

		supervisorPath := filepath.Join(parentPath, "supervisor")
		if err := os.MkdirAll(supervisorPath, 0755); err == nil {
			// Move ourselves to the supervisor cgroup
			procsPath := filepath.Join(supervisorPath, "cgroup.procs")
			if err := os.WriteFile(procsPath, []byte(strconv.Itoa(os.Getpid())), 0644); err == nil {
				// Now enable controllers in the parent (which is now empty)
				controlPath := filepath.Join(parentPath, "cgroup.subtree_control")
				if err := os.WriteFile(controlPath, []byte("+cpu +memory +pids"), 0644); err == nil {
					// Success! Return the parent as the base for service cgroups
					return parentPath, nil
				}
			}
		}

		// Fallback: try without moving (might work if already set up)
		path := filepath.Join(parentPath, "gosv")
		if err := os.MkdirAll(path, 0755); err == nil {
			return path, nil
		}
	}

	// Try 2: Direct root access (requires root, non-systemd)
	path := filepath.Join(cgroupRoot, "gosv")
	if err := os.MkdirAll(path, 0755); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("no writable cgroup location found - try running with: systemd-run --user --scope -p Delegate=yes ./gosv")
}

// NewCgroup creates a new cgroup for a process
func NewCgroup(name string) (*Cgroup, error) {
	if baseCgroupPath == "" {
		return nil, fmt.Errorf("cgroups not initialized - call EnsureControllers first")
	}

	path := filepath.Join(baseCgroupPath, name)

	// Create the cgroup directory
	// KEY CONCEPT: Creating a directory in cgroupfs creates a new cgroup
	// The kernel automatically populates it with control files
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cgroup: %w", err)
	}

	return &Cgroup{name: name, path: path}, nil
}

// AddProcess moves a process into this cgroup
func (c *Cgroup) AddProcess(pid int) error {
	// KEY CONCEPT: Writing PID to cgroup.procs moves it atomically
	// The process and ALL its threads move together
	// You cannot have threads in different cgroups (v2 rule)
	procsPath := filepath.Join(c.path, "cgroup.procs")
	return os.WriteFile(procsPath, []byte(strconv.Itoa(pid)), 0644)
}

// SetMemoryLimit sets the memory limit in bytes
func (c *Cgroup) SetMemoryLimit(bytes int64) error {
	if bytes <= 0 {
		return nil // No limit
	}

	// KEY CONCEPT: memory.max controls hard limit
	// When exceeded, kernel invokes OOM killer on processes in this cgroup
	// Alternative: memory.high is a "soft" limit that triggers reclaim pressure
	memPath := filepath.Join(c.path, "memory.max")
	return os.WriteFile(memPath, []byte(strconv.FormatInt(bytes, 10)), 0644)
}

// SetCPUQuota sets CPU quota as percentage (100 = 1 full core)
func (c *Cgroup) SetCPUQuota(percent int) error {
	if percent <= 0 {
		return nil // No limit
	}

	// KEY CONCEPT: cpu.max format is "quota period"
	// quota/period = fraction of CPU time
	// Example: "50000 100000" means 50000µs of CPU per 100000µs period = 50%
	// Example: "200000 100000" means 200% = 2 full cores
	//
	// Using 100ms (100000µs) period is standard - not too fine-grained
	period := 100000
	quota := (percent * period) / 100

	cpuPath := filepath.Join(c.path, "cpu.max")
	value := fmt.Sprintf("%d %d", quota, period)
	return os.WriteFile(cpuPath, []byte(value), 0644)
}

// SetPidsLimit limits the number of processes/threads
func (c *Cgroup) SetPidsLimit(max int) error {
	if max <= 0 {
		return nil
	}

	// KEY CONCEPT: pids.max prevents fork bombs
	// Applies to total tasks (processes + threads) in the cgroup tree
	pidsPath := filepath.Join(c.path, "pids.max")
	return os.WriteFile(pidsPath, []byte(strconv.Itoa(max)), 0644)
}

// GetMemoryUsage returns current memory usage in bytes
func (c *Cgroup) GetMemoryUsage() (int64, error) {
	data, err := os.ReadFile(filepath.Join(c.path, "memory.current"))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

// Destroy removes the cgroup
func (c *Cgroup) Destroy() error {
	// KEY CONCEPT: Can only remove empty cgroups
	// All processes must exit or move to another cgroup first
	// rmdir on the cgroup directory removes it
	return os.Remove(c.path)
}

// EnsureControllers finds a writable cgroup and enables required controllers
func EnsureControllers() error {
	// Find a cgroup location where we can create children
	path, err := findWritableCgroupBase()
	if err != nil {
		return err
	}

	baseCgroupPath = path

	// KEY CONCEPT: cgroup.subtree_control
	// Parent cgroup must enable controllers for children to use them
	// Write "+cpu +memory +pids" to enable those controllers
	controlPath := filepath.Join(baseCgroupPath, "cgroup.subtree_control")
	content := "+cpu +memory +pids"

	// Enable controllers for our child cgroups
	if err := os.WriteFile(controlPath, []byte(content), 0644); err != nil {
		// Not fatal - controllers might already be enabled or not available
		fmt.Printf("[gosv] note: could not enable all controllers: %v\n", err)
	}

	fmt.Printf("[gosv] using cgroup path: %s\n", baseCgroupPath)
	return nil
}

// CleanupCgroups removes the gosv cgroup directory
func CleanupCgroups() error {
	if baseCgroupPath == "" {
		return nil
	}
	// Try to remove our base cgroup (will fail if not empty, which is fine)
	return os.Remove(baseCgroupPath)
}
