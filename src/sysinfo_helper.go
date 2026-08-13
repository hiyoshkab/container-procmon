package procmon

import (
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
	pid        int
	cpuPercent float64
	memPercent float64
	rssBytes   uint64
}

func readCgroupMemLimit() (uint64, error) {
	// Try cgroups v2 first
	limitData, err := os.ReadFile(cgroupMemMaxPathV2)
	if err != nil {
		// Fall back to cgroups v1
		limitData, err = os.ReadFile(cgroupMemMaxPathV1)
		if err != nil {
			// TODO: Error handle
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
		// TODO: Error handle
	}

	return &Sampler{
		prev:     make(map[int]PrevCPU),
		memLimit: limit,
	}
}

func (sampler *Sampler) SampleProcs() ([]ProcStats, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		println("Error reading "+procRoot+" with error: ", err.Error())
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
			println("Error reading process stats for PID ", pid, " with error: ", err.Error())
		}

		// Process name always ends with ')'
		statsStr := string(statsData)
		procNameEndIdx := strings.LastIndex(statsStr, ")")
		if procNameEndIdx == -1 {
			println("Bad format for process name for PID ", pid)
			continue
		}

		// Extract each field after process name
		fields := strings.Fields(statsStr[procNameEndIdx+2:])
		if len(fields) < 22 {
			println("Bad format for process fields for PID ", pid)
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

		cpuPercent := 0.0
		prevProcStats, exists := sampler.prev[pid]
		if exists {
			deltaTicks := totalTicks - prevProcStats.totalTicks
			deltaTime := now.Sub(prevProcStats.timestamp).Seconds()

			if deltaTime > 0 {
				clkTck := uint64(100) // TODO: Dynamically retrieve clocktick
				cpuSeconds := float64(deltaTicks) / float64(clkTck)
				cpuPercent = cpuSeconds / deltaTime * 100.0
			}
		}

		memPercent := 0.0
		if sampler.memLimit > 0 {
			memPercent = float64(rssBytes) / float64(sampler.memLimit) * 100.0
		}

		statsList = append(statsList, ProcStats{
			pid:        pid,
			cpuPercent: cpuPercent,
			rssBytes:   rssBytes,
			memPercent: memPercent,
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
