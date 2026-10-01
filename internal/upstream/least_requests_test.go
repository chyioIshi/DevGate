package upstream_test

import (
	"net/url"
	"sync"
	"testing"

	"github.com/chyioishi/devgate/internal/upstream"
)

func TestNewLeastRequestsRejectsEmptyEndpoints(t *testing.T) {
	for _, endpoints := range [][]url.URL{nil, {}} {
		picker, err := upstream.NewLeastRequests(endpoints)
		if err == nil {
			t.Error("NewLeastRequests() error = nil, want non-nil")
		}
		if picker != nil {
			t.Errorf("NewLeastRequests() picker = %#v, want nil", picker)
		}
	}
}

func TestLeastRequestsRotatesEqualEndpoints(t *testing.T) {
	picker := newLeastRequests(t,
		url.URL{Scheme: "http", Host: "one.example"},
		url.URL{Scheme: "http", Host: "two.example"},
		url.URL{Scheme: "http", Host: "three.example"},
	)

	for i, want := range []string{
		"one.example",
		"two.example",
		"three.example",
		"one.example",
	} {
		target, release := picker.Acquire()
		if target.Host != want {
			t.Errorf("Acquire() call %d host = %q, want %q", i+1, target.Host, want)
		}
		release()
	}
}

func TestLeastRequestsSelectsEndpointWithFewestActiveRequests(t *testing.T) {
	picker := newLeastRequests(t,
		url.URL{Scheme: "http", Host: "one.example"},
		url.URL{Scheme: "http", Host: "two.example"},
		url.URL{Scheme: "http", Host: "three.example"},
	)

	_, releaseOne := picker.Acquire()
	_, releaseTwo := picker.Acquire()
	_, releaseThree := picker.Acquire()
	releaseTwo()

	target, release := picker.Acquire()
	if target.Host != "two.example" {
		t.Errorf("Acquire() host = %q, want least-loaded host %q", target.Host, "two.example")
	}

	release()
	releaseOne()
	releaseThree()
}

func TestLeastRequestsReleaseIsIdempotent(t *testing.T) {
	picker := newLeastRequests(t,
		url.URL{Scheme: "http", Host: "one.example"},
		url.URL{Scheme: "http", Host: "two.example"},
	)

	_, releaseOne := picker.Acquire()
	releaseOne()
	releaseOne()

	_, releaseTwo := picker.Acquire()
	releaseTwo()

	target, release := picker.Acquire()
	if target.Host != "one.example" {
		t.Errorf("Acquire() host after repeated release = %q, want %q", target.Host, "one.example")
	}
	release()
}

func TestLeastRequestsReplacePreservesActiveRequests(t *testing.T) {
	one := url.URL{Scheme: "http", Host: "one.example"}
	two := url.URL{Scheme: "http", Host: "two.example"}
	picker := newLeastRequests(t, one, two)

	_, releaseOne := picker.Acquire()
	if err := picker.Replace([]url.URL{one, two}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	target, releaseTwo := picker.Acquire()
	if target.Host != "two.example" {
		t.Fatalf("Acquire() host after Replace() = %q, want %q", target.Host, "two.example")
	}
	releaseTwo()

	target, release := picker.Acquire()
	if target.Host != "two.example" {
		t.Errorf("Acquire() host = %q, want preserved least-loaded host %q", target.Host, "two.example")
	}

	release()
	releaseOne()
}

func TestLeastRequestsRemovedEndpointCanStillBeReleased(t *testing.T) {
	one := url.URL{Scheme: "http", Host: "one.example"}
	two := url.URL{Scheme: "http", Host: "two.example"}
	picker := newLeastRequests(t, one, two)

	_, releaseRemoved := picker.Acquire()
	if err := picker.Replace([]url.URL{two}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	releaseRemoved()
	releaseRemoved()

	target, release := picker.Acquire()
	if target.Host != "two.example" {
		t.Errorf("Acquire() host = %q, want remaining host %q", target.Host, "two.example")
	}
	release()
}

func TestLeastRequestsReplaceRejectsEmptySnapshotAndKeepsCurrent(t *testing.T) {
	picker := newLeastRequests(t, url.URL{Scheme: "http", Host: "current.example"})

	for _, endpoints := range [][]url.URL{nil, {}} {
		if err := picker.Replace(endpoints); err == nil {
			t.Error("Replace() error = nil, want non-nil")
		}
		target, release := picker.Acquire()
		if target.Host != "current.example" {
			t.Errorf("Acquire() host after rejected Replace() = %q, want %q", target.Host, "current.example")
		}
		release()
	}
}

func TestLeastRequestsAcquireAndReplaceAreSafeForConcurrentUse(t *testing.T) {
	picker := newLeastRequests(t,
		url.URL{Scheme: "http", Host: "one.example"},
		url.URL{Scheme: "http", Host: "two.example"},
	)
	replacements := [][]url.URL{
		{{Scheme: "http", Host: "one.example"}, {Scheme: "http", Host: "two.example"}},
		{{Scheme: "http", Host: "two.example"}, {Scheme: "http", Host: "three.example"}},
	}

	const goroutineCount = 20
	var waitGroup sync.WaitGroup
	waitGroup.Add(goroutineCount + 1)
	for range goroutineCount {
		go func() {
			defer waitGroup.Done()
			for range 500 {
				_, release := picker.Acquire()
				release()
			}
		}()
	}
	go func() {
		defer waitGroup.Done()
		for i := range 1_000 {
			if err := picker.Replace(replacements[i%len(replacements)]); err != nil {
				t.Errorf("Replace() error = %v", err)
				return
			}
		}
	}()
	waitGroup.Wait()
}

func newLeastRequests(t *testing.T, endpoints ...url.URL) *upstream.LeastRequests {
	t.Helper()

	picker, err := upstream.NewLeastRequests(endpoints)
	if err != nil {
		t.Fatalf("NewLeastRequests() error = %v", err)
	}
	return picker
}
