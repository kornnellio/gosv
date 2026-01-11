// +build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// This demonstrates what happens WITHOUT proper zombie reaping
//
// KEY CONCEPT: Zombie processes
// When a process exits, its entry in the process table remains until
// the parent calls wait(). This is so the parent can retrieve the
// exit status. If the parent never calls wait(), the child becomes
// a "zombie" - taking up a process table slot forever.
//
// A zombie:
// - Uses no memory or CPU (it's dead)
// - DOES consume a process table entry
// - Shows as "Z" state in ps
// - Cannot be killed (already dead)
// - Only goes away when parent reaps it OR parent dies
//
// If parent dies, zombies are "reparented" to init (PID 1) which
// is responsible for reaping them.

func main() {
	fmt.Println("=== Zombie Process Demo ===")
	fmt.Printf("Parent PID: %d\n", os.Getpid())

	// Spawn a child that exits immediately
	cmd := exec.Command("/bin/sh", "-c", "echo 'Child exiting'; exit 0")
	if err := cmd.Start(); err != nil {
		fmt.Println("Error:", err)
		return
	}

	childPid := cmd.Process.Pid
	fmt.Printf("Started child PID: %d\n", childPid)

	// Wait a moment for child to exit
	time.Sleep(500 * time.Millisecond)

	// Check the child's state WITHOUT calling Wait()
	// It should be a zombie now
	fmt.Println("\nBEFORE calling Wait() - child is a zombie:")
	showProcessState(childPid)

	// Now do a ps to show zombies
	fmt.Println("\n'ps' output showing zombie:")
	psCmd := exec.Command("ps", "-o", "pid,ppid,state,comm", "-p", fmt.Sprintf("%d", childPid))
	psCmd.Stdout = os.Stdout
	psCmd.Stderr = os.Stderr
	psCmd.Run()

	// Now reap the zombie by calling Wait()
	fmt.Println("\nCalling Wait() to reap zombie...")
	err := cmd.Wait()
	if err != nil {
		fmt.Printf("Wait returned error (expected): %v\n", err)
	} else {
		fmt.Println("Wait returned successfully, exit code 0")
	}

	// Check again - process should be gone
	fmt.Println("\nAFTER calling Wait() - zombie is gone:")
	showProcessState(childPid)
}

func showProcessState(pid int) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		fmt.Printf("  Process %d no longer exists (reaped)\n", pid)
		return
	}

	// Parse state from /proc/[pid]/stat
	// Format: pid (comm) state ...
	// State is the 3rd field
	var p int
	var comm string
	var state rune
	fmt.Sscanf(string(data), "%d (%s %c", &p, &comm, &state)

	stateNames := map[rune]string{
		'R': "Running",
		'S': "Sleeping",
		'D': "Disk sleep (uninterruptible)",
		'Z': "ZOMBIE",
		'T': "Stopped",
		'X': "Dead",
	}

	fmt.Printf("  PID %d: state=%c (%s)\n", pid, state, stateNames[state])
}
