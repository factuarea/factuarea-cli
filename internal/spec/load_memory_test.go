package spec

import (
	"runtime"
	"testing"
)

func TestLoadDoesNotRetainParserGraphs(t *testing.T) {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < 3; i++ {
		if _, _, err := Load(); err != nil {
			t.Fatalf("Load: %v", err)
		}
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// A retained parser graph costs over 100 MiB per load for the real spec.
	// Allow 64 MiB for runtime caches while detecting accumulated graphs.
	const maxGrowth = 64 << 20
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("heap growth after three loads: %.1f MiB", float64(growth)/(1<<20))
	if growth > maxGrowth {
		t.Fatalf("Load retained %.1f MiB after GC; maximum allowed growth is 64 MiB", float64(growth)/(1<<20))
	}
}
