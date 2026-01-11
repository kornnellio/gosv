package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

// Config file format
type Config struct {
	Services []ServiceConfig `json:"services"`
}

type ServiceConfig struct {
	Name        string   `json:"name"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	MaxRestarts int      `json:"max_restarts"`
	MemoryMB    int      `json:"memory_mb"`
	CPUPercent  int      `json:"cpu_percent"`
}

func main() {
	configPath := flag.String("config", "", "Path to config file (JSON)")
	singleCmd := flag.String("run", "", "Run a single command")
	flag.Parse()

	// Show what we're about to do
	fmt.Println("=== gosv: Process Supervisor ===")
	fmt.Printf("PID: %d\n", os.Getpid())

	sup := NewSupervisor()

	if *configPath != "" {
		// Load from config file
		if err := loadConfig(sup, *configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
	} else if *singleCmd != "" {
		// Run a single command
		// Use "exec" so shell replaces itself with the command
		// This ensures the command is directly in our process group
		p := &Process{
			Name:          "main",
			Command:       "/bin/sh",
			Args:          []string{"-c", "exec " + *singleCmd},
			MaxRestarts:   10,
			RestartDelay:  2 * time.Second,
			BackoffFactor: 1.5,
		}
		sup.AddProcess(p)
	} else {
		// Demo mode: run some test processes
		fmt.Println("No config specified, running demo...")
		setupDemo(sup)
	}

	// Initialize cgroups (best effort)
	if err := EnsureControllers(); err != nil {
		fmt.Printf("[gosv] warning: cgroup setup failed: %v\n", err)
		fmt.Println("[gosv] continuing without resource limits")
	}

	if err := sup.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Supervisor error: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig(sup *Supervisor, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	for _, svc := range cfg.Services {
		p := &Process{
			Name:          svc.Name,
			Command:       svc.Command,
			Args:          svc.Args,
			MaxRestarts:   svc.MaxRestarts,
			RestartDelay:  time.Second,
			BackoffFactor: 2.0,
			MemoryLimit:   int64(svc.MemoryMB) * 1024 * 1024,
			CPUQuota:      svc.CPUPercent,
		}
		if p.MaxRestarts == 0 {
			p.MaxRestarts = 3
		}
		sup.AddProcess(p)
	}

	return nil
}

func setupDemo(sup *Supervisor) {
	// Demo: A process that prints and sleeps, will be restarted if killed
	demo := &Process{
		Name:          "heartbeat",
		Command:       "/bin/sh",
		Args:          []string{"-c", "while true; do echo '[heartbeat] alive at '$(date); sleep 2; done"},
		MaxRestarts:   5,
		RestartDelay:  time.Second,
		BackoffFactor: 2.0,
	}
	sup.AddProcess(demo)

	// Demo: A process that exits (to test restart)
	crasher := &Process{
		Name:          "crasher",
		Command:       "/bin/sh",
		Args:          []string{"-c", "echo '[crasher] starting...'; sleep 3; echo '[crasher] crashing!'; exit 1"},
		MaxRestarts:   3,
		RestartDelay:  2 * time.Second,
		BackoffFactor: 2.0,
	}
	sup.AddProcess(crasher)
}
