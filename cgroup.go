package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
type Cgroup struct {
	name string
	path string
}

const cgroupRoot = "/sys/fs/cgroup"

// NewCgroup creates a new cgroup under the root
func NewCgroup(name string) (*Cgroup, error) {
	path := filepath.Join(cgroupRoot, "gosv", name)

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
	return strconv.ParseInt(string(data[:len(data)-1]), 10, 64)
}

// Destroy removes the cgroup
func (c *Cgroup) Destroy() error {
	// KEY CONCEPT: Can only remove empty cgroups
	// All processes must exit or move to another cgroup first
	// rmdir on the cgroup directory removes it
	return os.Remove(c.path)
}

// EnsureControllers enables required controllers on parent cgroup
func EnsureControllers() error {
	// KEY CONCEPT: cgroup.subtree_control
	// Parent cgroup must enable controllers for children to use them
	// Write "+cpu +memory +pids" to enable those controllers

	gosvPath := filepath.Join(cgroupRoot, "gosv")
	if err := os.MkdirAll(gosvPath, 0755); err != nil {
		return err
	}

	// Enable controllers on root for our subtree
	// Note: This may fail if controllers aren't available
	controlPath := filepath.Join(cgroupRoot, "cgroup.subtree_control")
	content := "+cpu +memory +pids"

	// Best effort - some systems may not have all controllers
	_ = os.WriteFile(controlPath, []byte(content), 0644)

	return nil
}
