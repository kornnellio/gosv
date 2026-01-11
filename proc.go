package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcInfo contains information read from /proc/[pid]/*
//
// KEY CONCEPT: /proc filesystem (procfs)
// Virtual filesystem that exposes kernel data structures as files.
// Each process has a directory /proc/[pid]/ containing:
//   - status  : Human-readable process state
//   - stat    : Machine-parseable process stats (man 5 proc)
//   - maps    : Memory mappings (like pmap output)
//   - fd/     : Directory of open file descriptors
//   - cmdline : Command line arguments (null-separated)
//   - exe     : Symlink to actual executable
//   - cwd     : Symlink to current working directory
//   - environ : Environment variables (null-separated)
//
// Special directories:
//   - /proc/self : Symlink to current process
//   - /proc/sys  : Kernel tunables (sysctl)
type ProcInfo struct {
	PID        int
	Name       string
	State      string
	PPid       int
	Threads    int
	VmRSS      int64 // Resident memory in KB
	VmSize     int64 // Virtual memory in KB
	FDs        []FDInfo
	MemoryMaps []MemoryMap
}

type FDInfo struct {
	FD     int
	Path   string
	Mode   string // r, w, rw
}

type MemoryMap struct {
	Start    uint64
	End      uint64
	Perms    string // rwxp
	Pathname string
}

// ReadProcInfo reads process information from /proc/[pid]
func ReadProcInfo(pid int) (*ProcInfo, error) {
	procPath := fmt.Sprintf("/proc/%d", pid)

	// Check if process exists
	if _, err := os.Stat(procPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("process %d does not exist", pid)
	}

	info := &ProcInfo{PID: pid}

	// Read /proc/[pid]/status for human-readable info
	if err := info.readStatus(procPath); err != nil {
		return nil, err
	}

	// Read open file descriptors
	info.FDs = readFDs(procPath)

	// Read memory maps
	info.MemoryMaps = readMaps(procPath)

	return info, nil
}

// readStatus parses /proc/[pid]/status
func (p *ProcInfo) readStatus(procPath string) error {
	data, err := os.ReadFile(filepath.Join(procPath, "status"))
	if err != nil {
		return err
	}

	// KEY CONCEPT: /proc/[pid]/status format
	// Key-value pairs, one per line:
	//   Name:   bash
	//   State:  S (sleeping)
	//   Pid:    1234
	//   PPid:   1233
	//   Threads: 1
	//   VmRSS:  1234 kB
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Name":
			p.Name = val
		case "State":
			p.State = val
		case "PPid":
			p.PPid, _ = strconv.Atoi(val)
		case "Threads":
			p.Threads, _ = strconv.Atoi(val)
		case "VmRSS":
			// Format: "1234 kB"
			fields := strings.Fields(val)
			if len(fields) > 0 {
				p.VmRSS, _ = strconv.ParseInt(fields[0], 10, 64)
			}
		case "VmSize":
			fields := strings.Fields(val)
			if len(fields) > 0 {
				p.VmSize, _ = strconv.ParseInt(fields[0], 10, 64)
			}
		}
	}
	return nil
}

// readFDs reads /proc/[pid]/fd/*
func readFDs(procPath string) []FDInfo {
	fdPath := filepath.Join(procPath, "fd")
	entries, err := os.ReadDir(fdPath)
	if err != nil {
		return nil
	}

	var fds []FDInfo
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// KEY CONCEPT: /proc/[pid]/fd/N is a symlink to the actual file
		// For regular files: /path/to/file
		// For sockets: socket:[12345] (inode number)
		// For pipes: pipe:[12345]
		// For anonymous files: anon_inode:[eventfd] etc.
		target, err := os.Readlink(filepath.Join(fdPath, entry.Name()))
		if err != nil {
			continue
		}

		fds = append(fds, FDInfo{
			FD:   fd,
			Path: target,
		})
	}
	return fds
}

// readMaps reads /proc/[pid]/maps
func readMaps(procPath string) []MemoryMap {
	data, err := os.ReadFile(filepath.Join(procPath, "maps"))
	if err != nil {
		return nil
	}

	// KEY CONCEPT: /proc/[pid]/maps format
	// address           perms offset  dev   inode  pathname
	// 00400000-00401000 r-xp 00000000 08:01 123456 /bin/bash
	//
	// Address: start-end in hex
	// Perms: rwxp/s (read, write, execute, private/shared)
	// Offset: file offset (for mapped files)
	// Dev: device major:minor
	// Inode: inode number
	// Pathname: file path, or [heap], [stack], [vdso], etc.
	var maps []MemoryMap
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		addrParts := strings.Split(fields[0], "-")
		if len(addrParts) != 2 {
			continue
		}

		start, _ := strconv.ParseUint(addrParts[0], 16, 64)
		end, _ := strconv.ParseUint(addrParts[1], 16, 64)

		pathname := ""
		if len(fields) >= 6 {
			pathname = fields[5]
		}

		maps = append(maps, MemoryMap{
			Start:    start,
			End:      end,
			Perms:    fields[1],
			Pathname: pathname,
		})
	}
	return maps
}

// String formats ProcInfo for display
func (p *ProcInfo) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("PID: %d  Name: %s  State: %s\n", p.PID, p.Name, p.State))
	sb.WriteString(fmt.Sprintf("PPID: %d  Threads: %d\n", p.PPid, p.Threads))
	sb.WriteString(fmt.Sprintf("Memory: RSS=%d KB  Virtual=%d KB\n", p.VmRSS, p.VmSize))

	sb.WriteString(fmt.Sprintf("\nOpen file descriptors (%d):\n", len(p.FDs)))
	for _, fd := range p.FDs {
		sb.WriteString(fmt.Sprintf("  %3d -> %s\n", fd.FD, fd.Path))
	}

	// Show first 10 memory maps
	sb.WriteString(fmt.Sprintf("\nMemory maps (showing 10 of %d):\n", len(p.MemoryMaps)))
	for i, m := range p.MemoryMaps {
		if i >= 10 {
			break
		}
		sb.WriteString(fmt.Sprintf("  %012x-%012x %s %s\n", m.Start, m.End, m.Perms, m.Pathname))
	}

	return sb.String()
}

// Introspect prints detailed info about all supervised processes
func (s *Supervisor) Introspect() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.processes {
		if p.pid == 0 || p.state != StateRunning {
			continue
		}

		fmt.Printf("\n=== Process: %s ===\n", p.Name)
		info, err := ReadProcInfo(p.pid)
		if err != nil {
			fmt.Printf("Error reading proc info: %v\n", err)
			continue
		}
		fmt.Println(info.String())
	}
}
