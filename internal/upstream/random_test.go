package upstream_test

import (
	"net/url"
	"sync"
	"testing"

	"github.com/chyioishi/devgate/internal/upstream"
)

func TestNewRandomRejectsEmptyEndpoints(t *testing.T) {
	for _, endpoints := range [][]url.URL{nil, {}} {
		picker, err := upstream.NewRandom(endpoints)
		if err == nil {
			t.Error("NewRandom() error = nil, want non-nil")
		}
		if picker != nil {
			t.Errorf("NewRandom() picker = %#v, want nil", picker)
		}
	}
}

func TestRandomNextReturnsConfiguredEndpoint(t *testing.T) {
	endpoints := []url.URL{
		{Scheme: "http", Host: "one.example"},
		{Scheme: "http", Host: "two.example"},
		{Scheme: "http", Host: "three.example"},
	}
	picker, err := upstream.NewRandom(endpoints)
	if err != nil {
		t.Fatalf("NewRandom() error = %v", err)
	}
	validHosts := map[string]struct{}{
		"one.example":   {},
		"two.example":   {},
		"three.example": {},
	}

	for range 1_000 {
		host := picker.Next().Host
		if _, exists := validHosts[host]; !exists {
			t.Fatalf("Next() returned unknown host %q", host)
		}
	}
}

func TestRandomNextWithSingleEndpointIsStable(t *testing.T) {
	picker, err := upstream.NewRandom([]url.URL{
		{Scheme: "http", Host: "only.example"},
	})
	if err != nil {
		t.Fatalf("NewRandom() error = %v", err)
	}

	for range 100 {
		if got := picker.Next().Host; got != "only.example" {
			t.Fatalf("Next() host = %q, want %q", got, "only.example")
		}
	}
}

func TestRandomReplacePublishesNewSnapshot(t *testing.T) {
	picker, err := upstream.NewRandom([]url.URL{
		{Scheme: "http", Host: "initial.example"},
	})
	if err != nil {
		t.Fatalf("NewRandom() error = %v", err)
	}
	replacement := []url.URL{
		{Scheme: "http", Host: "replacement-one.example"},
		{Scheme: "http", Host: "replacement-two.example"},
	}
	if err := picker.Replace(replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	validHosts := map[string]struct{}{
		"replacement-one.example": {},
		"replacement-two.example": {},
	}

	for range 1_000 {
		host := picker.Next().Host
		if _, exists := validHosts[host]; !exists {
			t.Fatalf("Next() returned host %q from an inactive snapshot", host)
		}
	}
}

func TestRandomReplaceRejectsEmptySnapshotAndKeepsCurrent(t *testing.T) {
	picker, err := upstream.NewRandom([]url.URL{
		{Scheme: "http", Host: "current.example"},
	})
	if err != nil {
		t.Fatalf("NewRandom() error = %v", err)
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

func TestRandomReplaceCopiesEndpoints(t *testing.T) {
	picker, err := upstream.NewRandom([]url.URL{
		{Scheme: "http", Host: "initial.example"},
	})
	if err != nil {
		t.Fatalf("NewRandom() error = %v", err)
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

func TestRandomNextAndReplaceAreSafeForConcurrentUse(t *testing.T) {
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
	picker, err := upstream.NewRandom(snapshots[0])
	if err != nil {
		t.Fatalf("NewRandom() error = %v", err)
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
