package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	procRoot           = "/proc"
	cgroupMemMaxPathV2 = "/sys/fs/cgroup/memory.max"
	cgroupMemMaxPathV1 = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
)

type PrevCPU struct {
	totalTicks uint64
	timestamp  time.Time
}

type Sampler struct {
	prev     map[int]PrevCPU
	memLimit uint64
}

type ProcStats struct {
	pid            int
	name           string
	cpuUtilization float64
	memUtilization float64
	rssBytes       uint64
}

func readCgroupMemLimit() (uint64, error) {
	// Try cgroups v2 first
	limitData, err := os.ReadFile(cgroupMemMaxPathV2)
	if err != nil {
		// Fall back to cgroups v1
		limitData, err = os.ReadFile(cgroupMemMaxPathV1)
		if err != nil {
			slog.Error("reading cgroup memory limit failed", "err", err)
			return 0, err
		}
	}

	limitStr := strings.TrimSpace(string(limitData))

	var memLimit uint64
	if limitStr == "max" {
		memLimit = 0 // unlimited
	} else {
		memLimit, _ = strconv.ParseUint(limitStr, 10, 64)
	}

	return memLimit, nil
}

func NewSampler() *Sampler {
	limit, err := readCgroupMemLimit()
	if err != nil {
		slog.Warn("could not determine memory limit, memory utilization will be 0", "err", err)
	}

	return &Sampler{
		prev:     make(map[int]PrevCPU),
		memLimit: limit,
	}
}

func (sampler *Sampler) SampleProcs() ([]ProcStats, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		slog.Error("reading proc root failed", "path", procRoot, "err", err)
		return nil, err
	}

	// Track seen Pids so we can remove stale data
	seen := make(map[int]bool)

	// Return Value List
	statsList := []ProcStats{}

	// Now
	now := time.Now()

	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			// Skip if not a PID
			continue
		}

		seen[pid] = true

		statsPath := filepath.Join(procRoot, strconv.Itoa(pid), "stat")
		statsData, err := os.ReadFile(statsPath)
		if err != nil {
			slog.Debug("reading process stat failed", "pid", pid, "err", err)
			continue
		}

		// Process name always ends with ')'
		statsStr := string(statsData)
		procNameStartIdx := strings.Index(statsStr, "(")
		procNameEndIdx := strings.LastIndex(statsStr, ")")

		if procNameEndIdx == -1 {
			slog.Debug("bad format for process name", "pid", pid)
			continue
		}

		procName := statsStr[procNameStartIdx+1 : procNameEndIdx]

		// Extract each field after process name
		fields := strings.Fields(statsStr[procNameEndIdx+2:])
		if len(fields) < 22 {
			slog.Debug("bad format for process fields", "pid", pid)
			continue
		}

		// Subtract 3 to get list index
		// #14 CPU user mode
		// #15 CPU system mode
		// #23 RSS
		uTicks, _ := strconv.ParseUint(fields[11], 10, 64)
		sTicks, _ := strconv.ParseUint(fields[12], 10, 64)
		rssPages, _ := strconv.ParseUint(fields[21], 10, 64)
		rssBytes := rssPages * uint64(os.Getpagesize())

		totalTicks := uTicks + sTicks

		cpuUtilization := 0.0
		prevProcStats, exists := sampler.prev[pid]
		if exists {
			deltaTicks := totalTicks - prevProcStats.totalTicks
			deltaTime := now.Sub(prevProcStats.timestamp).Seconds()

			if deltaTime > 0 {
				clkTck := uint64(100) // TODO: Dynamically retrieve clocktick
				cpuSeconds := float64(deltaTicks) / float64(clkTck)
				cpuUtilization = cpuSeconds / deltaTime
			}
		}

		memUtilization := 0.0
		if sampler.memLimit > 0 {
			memUtilization = float64(rssBytes) / float64(sampler.memLimit)
		}

		statsList = append(statsList, ProcStats{
			pid:            pid,
			name:           procName,
			cpuUtilization: cpuUtilization,
			rssBytes:       rssBytes,
			memUtilization: memUtilization,
		})

		// Update prevSample for next sample
		sampler.prev[pid] = PrevCPU{
			totalTicks: totalTicks,
			timestamp:  now,
		}
	}

	// If PID is no longer present, remove it from the previous sample
	for p := range sampler.prev {
		if !seen[p] {
			delete(sampler.prev, p)
		}
	}

	return statsList, nil
}
