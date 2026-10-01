package upstream_test

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/chyioishi/devgate/internal/upstream"
)

func BenchmarkRoundRobinNext(b *testing.B) {
	picker := newBenchmarkRoundRobin(b, 10)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = picker.Next()
	}
}

func BenchmarkRoundRobinNextParallel(b *testing.B) {
	picker := newBenchmarkRoundRobin(b, 10)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = picker.Next()
		}
	})
}

func BenchmarkRoundRobinReplace(b *testing.B) {
	picker := newBenchmarkRoundRobin(b, 10)
	endpoints := benchmarkEndpoints(10)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := picker.Replace(endpoints); err != nil {
			b.Fatalf("Replace() error = %v", err)
		}
	}
}

func newBenchmarkRoundRobin(b *testing.B, endpointCount int) *upstream.RoundRobin {
	b.Helper()

	endpoints := benchmarkEndpoints(endpointCount)
	picker, err := upstream.NewRoundRobin(endpoints)
	if err != nil {
		b.Fatalf("NewRoundRobin() error = %v", err)
	}
	return picker
}

func benchmarkEndpoints(endpointCount int) []url.URL {
	endpoints := make([]url.URL, endpointCount)
	for i := range endpoints {
		endpoints[i] = url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("upstream-%d.example", i),
		}
	}
	return endpoints
}
