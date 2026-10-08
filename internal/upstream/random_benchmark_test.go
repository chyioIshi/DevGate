package upstream_test

import (
	"testing"

	"github.com/chyioishi/devgate/internal/upstream"
)

func BenchmarkRandomAcquire(b *testing.B) {
	picker := newBenchmarkRandom(b, 10)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, release, _ := picker.Acquire()
		release()
	}
}

func BenchmarkRandomAcquireParallel(b *testing.B) {
	picker := newBenchmarkRandom(b, 10)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, release, _ := picker.Acquire()
			release()
		}
	})
}

func BenchmarkRandomReplace(b *testing.B) {
	picker := newBenchmarkRandom(b, 10)
	endpoints := benchmarkEndpoints(10)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := picker.Replace(endpoints); err != nil {
			b.Fatalf("Replace() error = %v", err)
		}
	}
}

func newBenchmarkRandom(b *testing.B, endpointCount int) *upstream.Random {
	b.Helper()

	picker, err := upstream.NewRandom(benchmarkEndpoints(endpointCount))
	if err != nil {
		b.Fatalf("NewRandom() error = %v", err)
	}
	return picker
}
