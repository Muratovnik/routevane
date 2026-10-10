//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func main() {
	if err := probe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func probe() error {
	if err := os.WriteFile("started", nil, 0o600); err != nil {
		return err
	}
	limits := make(map[string]unix.Rlimit)
	var supervisorAddressSpace unix.Rlimit
	if err := unix.Prlimit(os.Getppid(), unix.RLIMIT_AS, nil, &supervisorAddressSpace); err != nil {
		return err
	}
	for name, resource := range map[string]int{
		"address_space": unix.RLIMIT_AS, "cpu": unix.RLIMIT_CPU,
		"files": unix.RLIMIT_NOFILE, "core": unix.RLIMIT_CORE,
	} {
		var value unix.Rlimit
		if err := unix.Getrlimit(resource, &value); err != nil {
			return err
		}
		limits[name] = value
	}
	mapping, allocationError := unix.Mmap(-1, 0, 2<<30, unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if allocationError == nil {
		if err := unix.Munmap(mapping); err != nil {
			return err
		}
	}
	files := make([]*os.File, 0, 129)
	var openError error
	for range 129 {
		file, err := os.Open("/dev/null")
		if err != nil {
			openError = err
			break
		}
		files = append(files, file)
	}
	for _, file := range files {
		if err := file.Close(); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Limits                 map[string]unix.Rlimit
		AllocationRefused      bool
		OpenRefused            bool
		PID                    int
		Group                  int
		SupervisorAddressSpace unix.Rlimit
	}{limits, errors.Is(allocationError, unix.ENOMEM), errors.Is(openError, unix.EMFILE), os.Getpid(), unix.Getpgrp(), supervisorAddressSpace})
}
