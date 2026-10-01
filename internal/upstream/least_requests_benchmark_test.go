package upstream_test

import (
	"testing"

	"github.com/chyioishi/devgate/internal/upstream"
)

func BenchmarkLeastRequestsAcquire(b *testing.B) {
	picker := newBenchmarkLeastRequests(b, 10)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, release := picker.Acquire()
		release()
	}
}

func BenchmarkLeastRequestsAcquireParallel(b *testing.B) {
	picker := newBenchmarkLeastRequests(b, 10)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, release := picker.Acquire()
			release()
		}
	})
}

func BenchmarkLeastRequestsReplace(b *testing.B) {
	picker := newBenchmarkLeastRequests(b, 10)
	endpoints := benchmarkEndpoints(10)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := picker.Replace(endpoints); err != nil {
			b.Fatalf("Replace() error = %v", err)
		}
	}
}

func newBenchmarkLeastRequests(b *testing.B, endpointCount int) *upstream.LeastRequests {
	b.Helper()

	picker, err := upstream.NewLeastRequests(benchmarkEndpoints(endpointCount))
	if err != nil {
		b.Fatalf("NewLeastRequests() error = %v", err)
	}
	return picker
}
