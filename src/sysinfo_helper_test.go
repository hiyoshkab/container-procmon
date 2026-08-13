package procmon

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadCgroupMemLimitUnlimited(t *testing.T) {
	dir := t.TempDir()
	memPath := filepath.Join(dir, "memory.max")
	if err := os.WriteFile(memPath, []byte("max\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldPath := cgroupMemMaxPathV2
	cgroupMemMaxPathV2 = memPath
	defer func() { cgroupMemMaxPathV2 = oldPath }()

	got, err := readCgroupMemLimit()
	if err != nil {
		t.Fatalf("readCgroupMemLimit() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("readCgroupMemLimit() = %d, want 0 (unlimited)", got)
	}
}

func TestSampleProcs(t *testing.T) {
	dir := t.TempDir()
	procDir := filepath.Join(dir, "proc")
	if err := os.MkdirAll(filepath.Join(procDir, "123"), 0o755); err != nil {
		t.Fatal(err)
	}

	statContent := "123 (procname) S 1 1 1 0 0 0 0 0 0 0 100 200 0 0 0 0 0 0 0 0 12345\n"
	if err := os.WriteFile(filepath.Join(procDir, "123", "stat"), []byte(statContent), 0o644); err != nil {
		t.Fatal(err)
	}

	oldProcRoot := procRoot
	procRoot = procDir
	defer func() { procRoot = oldProcRoot }()

	sampler := NewSampler()
	sampler.memLimit = 1073741824 //1 GiB
	sampler.prev[123] = PrevCPU{
		totalTicks: 200,
		timestamp:  time.Now().Add(-1 * time.Second),
	}

	// Check that SamplePRocs returns the expected stats for the mocked process
	stats, err := sampler.SampleProcs()
	if err != nil {
		t.Fatalf("SampleProcs() error = %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("SampleProcs() returned %d stats, want 1", len(stats))
	}
	if stats[0].pid != 123 {
		t.Fatalf("pid = %d, want 123", stats[0].pid)
	}

	// Check RSS bytes: 12345 pages * pagesize
	pagesize := uint64(os.Getpagesize())
	expectedRSSBytes := uint64(12345) * pagesize
	if stats[0].rssBytes != expectedRSSBytes {
		t.Fatalf("rssBytes = %d, want %d", stats[0].rssBytes, expectedRSSBytes)
	}

	// Check CPU percent: totalTicks changed from 200 to 300 (100 delta) over ~1 second
	// cpuPercent = (100 / 100) / 1 * 100 = ~100% (with tolerance for timing)
	if math.Abs(stats[0].cpuPercent-100.0) > 0.1 {
		t.Fatalf("cpuPercent = %f, want ~100.0", stats[0].cpuPercent)
	}

	// Check memory percent: rssBytes / memLimit * 100
	expectedMemPercent := float64(expectedRSSBytes) / float64(sampler.memLimit) * 100.0
	if stats[0].memPercent != expectedMemPercent {
		t.Fatalf("memPercent = %f, want %f", stats[0].memPercent, expectedMemPercent)
	}
}

func TestSeenMapCleansUpStalePIDs(t *testing.T) {
	dir := t.TempDir()
	procDir := filepath.Join(dir, "proc")

	// Create two processes initially
	for _, pid := range []string{"123", "456"} {
		if err := os.MkdirAll(filepath.Join(procDir, pid), 0o755); err != nil {
			t.Fatal(err)
		}
		statContent := "1 (proc) S 1 1 1 0 0 0 0 0 0 0 100 200 0 0 0 0 0 0 0 0 10245\n"
		if err := os.WriteFile(filepath.Join(procDir, pid, "stat"), []byte(statContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	oldProcRoot := procRoot
	procRoot = procDir
	defer func() { procRoot = oldProcRoot }()

	sampler := NewSampler()
	sampler.memLimit = 1073741824

	// First sample: both processes exist
	sampler.SampleProcs()
	if len(sampler.prev) != 2 {
		t.Fatalf("After first sample, prev has %d entries, want 2", len(sampler.prev))
	}

	// Remove PID 456
	os.RemoveAll(filepath.Join(procDir, "456"))

	// Second sample: only PID 123 exists, PID 456 should be cleaned up
	sampler.SampleProcs()
	if len(sampler.prev) != 1 {
		t.Fatalf("After second sample, prev has %d entries, want 1", len(sampler.prev))
	}
	if _, exists := sampler.prev[456]; exists {
		t.Fatalf("PID 456 should have been removed from prev")
	}
	if _, exists := sampler.prev[123]; !exists {
		t.Fatalf("PID 123 should still be in prev")
	}
}
