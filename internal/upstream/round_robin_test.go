package upstream_test

import (
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chyioishi/devgate/internal/upstream"
)

func TestNewRoundRobinRejectsEmptyEndpoints(t *testing.T) {
	for _, endpoints := range [][]url.URL{nil, {}} {
		picker, err := upstream.NewRoundRobin(endpoints)
		if err == nil {
			t.Error("NewRoundRobin() error = nil, want non-nil")
		}
		if picker != nil {
			t.Errorf("NewRoundRobin() picker = %#v, want nil", picker)
		}
	}
}

func TestRoundRobinCyclesThroughEndpoints(t *testing.T) {
	endpoints := []url.URL{
		{Scheme: "http", Host: "one.example"},
		{Scheme: "http", Host: "two.example"},
		{Scheme: "http", Host: "three.example"},
	}
	picker, err := upstream.NewRoundRobin(endpoints)
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}

	wantHosts := []string{
		"one.example",
		"two.example",
		"three.example",
		"one.example",
		"two.example",
		"three.example",
		"one.example",
	}
	for i, wantHost := range wantHosts {
		if got := picker.Next().Host; got != wantHost {
			t.Errorf("Next() call %d host = %q, want %q", i+1, got, wantHost)
		}
	}
}

func TestRoundRobinCopiesEndpoints(t *testing.T) {
	endpoints := []url.URL{
		{Scheme: "http", Host: "one.example"},
		{Scheme: "http", Host: "two.example"},
	}
	picker, err := upstream.NewRoundRobin(endpoints)
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}

	endpoints[0].Host = "mutated.example"
	first := picker.Next()
	if first.Host != "one.example" {
		t.Errorf("Next() host after input mutation = %q, want %q", first.Host, "one.example")
	}

	first.Host = "mutated-return.example"
	_ = picker.Next()
	if got := picker.Next().Host; got != "one.example" {
		t.Errorf("Next() host after returned value mutation = %q, want %q", got, "one.example")
	}
}

func TestRoundRobinIsSafeForConcurrentUse(t *testing.T) {
	const (
		endpointCount     = 4
		goroutineCount    = 100
		callsPerGoroutine = 100
	)
	endpoints := []url.URL{
		{Scheme: "http", Host: "one.example"},
		{Scheme: "http", Host: "two.example"},
		{Scheme: "http", Host: "three.example"},
		{Scheme: "http", Host: "four.example"},
	}
	picker, err := upstream.NewRoundRobin(endpoints)
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}
	hostIndexes := map[string]int{
		"one.example":   0,
		"two.example":   1,
		"three.example": 2,
		"four.example":  3,
	}
	var counts [endpointCount]atomic.Int64
	var waitGroup sync.WaitGroup
	waitGroup.Add(goroutineCount)

	for range goroutineCount {
		go func() {
			defer waitGroup.Done()
			for range callsPerGoroutine {
				endpoint := picker.Next()
				index, ok := hostIndexes[endpoint.Host]
				if !ok {
					t.Errorf("Next() returned unknown host %q", endpoint.Host)
					continue
				}
				counts[index].Add(1)
			}
		}()
	}
	waitGroup.Wait()

	wantCount := int64(goroutineCount * callsPerGoroutine / endpointCount)
	for i := range counts {
		if got := counts[i].Load(); got != wantCount {
			t.Errorf("endpoint %d selections = %d, want %d", i, got, wantCount)
		}
	}
}
