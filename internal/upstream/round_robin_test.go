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

func TestRoundRobinReplacePublishesNewSnapshot(t *testing.T) {
	initial := []url.URL{
		{Scheme: "http", Host: "initial-one.example"},
		{Scheme: "http", Host: "initial-two.example"},
	}
	picker, err := upstream.NewRoundRobin(initial)
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}
	if got := picker.Next().Host; got != "initial-one.example" {
		t.Fatalf("initial Next() host = %q, want %q", got, "initial-one.example")
	}

	replacement := []url.URL{
		{Scheme: "http", Host: "replacement-one.example"},
		{Scheme: "http", Host: "replacement-two.example"},
		{Scheme: "http", Host: "replacement-three.example"},
	}
	if err := picker.Replace(replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	// The selection counter is intentionally preserved. The first selection
	// used index 0, so the next selection uses index 1 in the new snapshot.
	if got := picker.Next().Host; got != "replacement-two.example" {
		t.Errorf("Next() host after Replace() = %q, want %q", got, "replacement-two.example")
	}
}

func TestRoundRobinReplaceRejectsEmptySnapshotAndKeepsCurrent(t *testing.T) {
	picker, err := upstream.NewRoundRobin([]url.URL{
		{Scheme: "http", Host: "current.example"},
	})
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}

	for _, endpoints := range [][]url.URL{nil, {}} {
		if err := picker.Replace(endpoints); err == nil {
			t.Error("Replace() error = nil, want non-nil")
		}
		if got := picker.Next().Host; got != "current.example" {
			t.Errorf("Next() host after rejected Replace() = %q, want %q", got, "current.example")
		}
	}
}

func TestRoundRobinReplaceCopiesEndpoints(t *testing.T) {
	picker, err := upstream.NewRoundRobin([]url.URL{
		{Scheme: "http", Host: "initial.example"},
	})
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}
	replacement := []url.URL{
		{Scheme: "http", Host: "replacement.example"},
	}
	if err := picker.Replace(replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	replacement[0].Host = "mutated-input.example"
	got := picker.Next()
	if got.Host != "replacement.example" {
		t.Errorf("Next() host after input mutation = %q, want %q", got.Host, "replacement.example")
	}

	got.Host = "mutated-result.example"
	if next := picker.Next().Host; next != "replacement.example" {
		t.Errorf("Next() host after result mutation = %q, want %q", next, "replacement.example")
	}
}

func TestRoundRobinNextAndReplaceAreSafeForConcurrentUse(t *testing.T) {
	const (
		readerCount    = 20
		callsPerReader = 500
		replaceCount   = 1_000
	)
	snapshots := [][]url.URL{
		{
			{Scheme: "http", Host: "a-one.example"},
			{Scheme: "http", Host: "a-two.example"},
		},
		{
			{Scheme: "http", Host: "b-one.example"},
			{Scheme: "http", Host: "b-two.example"},
			{Scheme: "http", Host: "b-three.example"},
		},
	}
	picker, err := upstream.NewRoundRobin(snapshots[0])
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}
	validHosts := map[string]struct{}{
		"a-one.example":   {},
		"a-two.example":   {},
		"b-one.example":   {},
		"b-two.example":   {},
		"b-three.example": {},
	}

	var waitGroup sync.WaitGroup
	waitGroup.Add(readerCount + 1)
	for range readerCount {
		go func() {
			defer waitGroup.Done()
			for range callsPerReader {
				host := picker.Next().Host
				if _, exists := validHosts[host]; !exists {
					t.Errorf("Next() returned unknown host %q", host)
				}
			}
		}()
	}
	go func() {
		defer waitGroup.Done()
		for i := range replaceCount {
			if err := picker.Replace(snapshots[i%len(snapshots)]); err != nil {
				t.Errorf("Replace() error = %v", err)
				return
			}
		}
	}()
	waitGroup.Wait()
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
