package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/upstream"
)

func TestRouteHealthCheckObserverRefreshesPickerOnTransition(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://user:secret@upstream.example")
	tracker, err := upstream.NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	base, err := upstream.NewRoundRobin([]url.URL{target})
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}
	picker, err := upstream.NewHealthAwarePicker(base, tracker)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	var logs bytes.Buffer
	observer := &routeHealthCheckObserver{
		routeName: "users",
		picker:    picker,
		logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	tracker.RecordFailure(target)

	observer.RecordStateTransition(target, false)

	_, release, err := picker.Acquire()
	if !errors.Is(err, upstream.ErrNoHealthyEndpoints) {
		t.Errorf("Acquire() error = %v, want ErrNoHealthyEndpoints", err)
	}
	if release != nil {
		t.Error("Acquire() release is non-nil, want nil")
	}
	records := decodeObserverLogs(t, logs.Bytes())
	if len(records) != 1 {
		t.Fatalf("log record count = %d, want 1", len(records))
	}
	assertObserverLogFields(t, records[0], "users", target.Redacted(), false)
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("logs expose target password: %s", logs.String())
	}
}

func TestRouteHealthCheckObserverLogsRefreshFailure(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://user:secret@upstream.example")
	tracker, err := upstream.NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	base := &observerTestPicker{}
	picker, err := upstream.NewHealthAwarePicker(base, tracker)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	base.replaceErr = errors.New("replace failed")
	var logs bytes.Buffer
	observer := &routeHealthCheckObserver{
		routeName: "users",
		picker:    picker,
		logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	observer.RecordStateTransition(target, true)

	_, release, err := picker.Acquire()
	if !errors.Is(err, upstream.ErrNoHealthyEndpoints) {
		t.Errorf("Acquire() error = %v, want ErrNoHealthyEndpoints", err)
	}
	if release != nil {
		t.Error("Acquire() release is non-nil, want nil")
	}
	records := decodeObserverLogs(t, logs.Bytes())
	if len(records) != 2 {
		t.Fatalf("log record count = %d, want 2", len(records))
	}
	assertObserverLogFields(t, records[0], "users", target.Redacted(), true)
	if got := records[1]["route"]; got != "users" {
		t.Errorf("error log route = %v, want %q", got, "users")
	}
	if got := records[1]["target"]; got != target.Redacted() {
		t.Errorf("error log target = %v, want %q", got, target.Redacted())
	}
	if got := records[1]["msg"]; got != "failed to refresh healthy upstream picker" {
		t.Errorf("error log message = %v, want refresh failure message", got)
	}
	if got := records[1]["error"]; got != "replace healthy endpoints: replace failed" {
		t.Errorf("error log error = %v, want wrapped replace error", got)
	}
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("logs expose target password: %s", logs.String())
	}
}

type observerTestPicker struct {
	target     url.URL
	replaceErr error
}

func (p *observerTestPicker) Acquire() (url.URL, func(), error) {
	return p.target, func() {}, nil
}

func (p *observerTestPicker) Replace(endpoints []url.URL) error {
	if p.replaceErr != nil {
		return p.replaceErr
	}
	p.target = endpoints[0]
	return nil
}

func mustParseObserverTarget(t *testing.T, rawURL string) url.URL {
	t.Helper()

	target, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", rawURL, err)
	}
	return *target
}

func decodeObserverLogs(t *testing.T, data []byte) []map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	records := make([]map[string]any, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &records[i]); err != nil {
			t.Fatalf("decode log record %d: %v", i, err)
		}
	}
	return records
}

func assertObserverLogFields(
	t *testing.T,
	record map[string]any,
	routeName string,
	target string,
	healthy bool,
) {
	t.Helper()

	if got := record["route"]; got != routeName {
		t.Errorf("log route = %v, want %q", got, routeName)
	}
	if got := record["target"]; got != target {
		t.Errorf("log target = %v, want %q", got, target)
	}
	if got := record["healthy"]; got != healthy {
		t.Errorf("log healthy = %v, want %t", got, healthy)
	}
}
