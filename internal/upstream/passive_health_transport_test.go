package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/proxytarget"
)

func TestClassifyPassiveHealthOutcome(t *testing.T) {
	t.Parallel()

	request := &http.Request{}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	canceledRequest := request.WithContext(canceledContext)
	transportErr := errors.New("transport failure")

	tests := []struct {
		name    string
		request *http.Request
		resp    *http.Response
		err     error
		want    passiveHealthOutcome
	}{
		{
			name:    "canceled request takes precedence over error and response",
			request: canceledRequest,
			resp:    &http.Response{StatusCode: http.StatusServiceUnavailable},
			err:     transportErr,
			want:    passiveHealthOutcomeIgnored,
		},
		{
			name:    "transport error",
			request: request,
			err:     transportErr,
			want:    passiveHealthOutcomeFailure,
		},
		{
			name:    "missing response",
			request: request,
			want:    passiveHealthOutcomeIgnored,
		},
		{
			name:    "successful response",
			request: request,
			resp:    &http.Response{StatusCode: http.StatusNoContent},
			want:    passiveHealthOutcomeSuccess,
		},
		{
			name:    "client error response",
			request: request,
			resp:    &http.Response{StatusCode: http.StatusNotFound},
			want:    passiveHealthOutcomeSuccess,
		},
		{
			name:    "status immediately below server error range",
			request: request,
			resp:    &http.Response{StatusCode: 499},
			want:    passiveHealthOutcomeSuccess,
		},
		{
			name:    "first server error status",
			request: request,
			resp:    &http.Response{StatusCode: 500},
			want:    passiveHealthOutcomeFailure,
		},
		{
			name:    "last server error status",
			request: request,
			resp:    &http.Response{StatusCode: 599},
			want:    passiveHealthOutcomeFailure,
		},
		{
			name:    "status immediately above server error range",
			request: request,
			resp:    &http.Response{StatusCode: 600},
			want:    passiveHealthOutcomeSuccess,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := classifyPassiveHealthOutcome(test.request, test.resp, test.err)
			if got != test.want {
				t.Errorf("classifyPassiveHealthOutcome() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestPassiveHealthTransportRejectsRequestWithoutSelectedTarget(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{target}, 1, time.Second)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	baseCalls := 0
	base := passiveHealthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls++
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})
	transport, err := NewPassiveHealthTransport(base, tracker, noopPassiveHealthObserver{})
	if err != nil {
		t.Fatalf("NewPassiveHealthTransport() error = %v", err)
	}

	resp, err := transport.RoundTrip(&http.Request{})
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if !errors.Is(err, errPassiveHealthTargetMissing) {
		t.Errorf("RoundTrip() error = %v, want errPassiveHealthTargetMissing", err)
	}
	if resp != nil {
		t.Errorf("RoundTrip() response = %#v, want nil", resp)
	}
	if baseCalls != 0 {
		t.Errorf("base RoundTrip() calls = %d, want 0", baseCalls)
	}
}

func TestPassiveHealthTransportRejectsUntrackedTarget(t *testing.T) {
	t.Parallel()

	tracked := url.URL{Scheme: "http", Host: "tracked.example"}
	untracked := url.URL{Scheme: "http", Host: "untracked.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{tracked}, 1, time.Second)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	baseCalls := 0
	base := passiveHealthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls++
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})
	transport, err := NewPassiveHealthTransport(base, tracker, noopPassiveHealthObserver{})
	if err != nil {
		t.Fatalf("NewPassiveHealthTransport() error = %v", err)
	}

	resp, err := transport.RoundTrip(passiveHealthRequest(untracked))
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if !errors.Is(err, ErrNoHealthyEndpoints) {
		t.Errorf("RoundTrip() error = %v, want ErrNoHealthyEndpoints", err)
	}
	if resp != nil {
		t.Errorf("RoundTrip() response = %#v, want nil", resp)
	}
	if baseCalls != 0 {
		t.Errorf("base RoundTrip() calls = %d, want 0", baseCalls)
	}
}

func TestPassiveHealthTransportEjectsAtThresholdAndRejectsFurtherRequests(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{target}, 2, time.Minute)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	baseCalls := 0
	base := passiveHealthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		baseCalls++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
	})
	observer := &recordingPassiveHealthObserver{}
	transport, err := NewPassiveHealthTransport(base, tracker, observer)
	if err != nil {
		t.Fatalf("NewPassiveHealthTransport() error = %v", err)
	}
	request := passiveHealthRequest(target)

	firstResponse, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("first RoundTrip() error = %v", err)
	}
	defer func() { _ = firstResponse.Body.Close() }()
	if got := observer.ejections; len(got) != 0 {
		t.Errorf("ejections after first failure = %v, want none", got)
	}
	secondResponse, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("second RoundTrip() error = %v", err)
	}
	defer func() { _ = secondResponse.Body.Close() }()
	if got := observer.ejections; !slices.Equal(got, []url.URL{target}) {
		t.Errorf("ejections after threshold = %v, want %v", got, []url.URL{target})
	}

	resp, err := transport.RoundTrip(request)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if !errors.Is(err, ErrNoHealthyEndpoints) {
		t.Errorf("RoundTrip() after ejection error = %v, want ErrNoHealthyEndpoints", err)
	}
	if resp != nil {
		t.Errorf("RoundTrip() after ejection response = %#v, want nil", resp)
	}
	if baseCalls != 2 {
		t.Errorf("base RoundTrip() calls = %d, want 2", baseCalls)
	}
	if got := observer.ejections; len(got) != 1 {
		t.Errorf("ejection notifications = %d, want 1", len(got))
	}
}

func TestPassiveHealthTransportSuccessResetsFailureStreak(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{target}, 2, time.Minute)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	statuses := []int{
		http.StatusInternalServerError,
		http.StatusNoContent,
		http.StatusInternalServerError,
		http.StatusInternalServerError,
	}
	base := passiveHealthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		status := statuses[0]
		statuses = statuses[1:]
		return &http.Response{StatusCode: status, Body: http.NoBody}, nil
	})
	observer := &recordingPassiveHealthObserver{}
	transport, err := NewPassiveHealthTransport(base, tracker, observer)
	if err != nil {
		t.Fatalf("NewPassiveHealthTransport() error = %v", err)
	}
	request := passiveHealthRequest(target)

	for attempt := range 3 {
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("RoundTrip() attempt %d error = %v", attempt+1, err)
		}
		defer func() { _ = response.Body.Close() }()
	}
	if got := observer.ejections; len(got) != 0 {
		t.Errorf("ejections before consecutive threshold = %v, want none", got)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("fourth RoundTrip() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if got := observer.ejections; !slices.Equal(got, []url.URL{target}) {
		t.Errorf("ejections after consecutive threshold = %v, want %v", got, []url.URL{target})
	}
}

func TestNewPassiveHealthTransportRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{target}, 1, time.Second)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	base := passiveHealthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	})
	observer := noopPassiveHealthObserver{}

	tests := []struct {
		name        string
		base        http.RoundTripper
		tracker     *PassiveHealthTracker
		observer    PassiveHealthObserver
		wantMessage string
	}{
		{
			name:        "missing base transport",
			tracker:     tracker,
			observer:    observer,
			wantMessage: "base transport cannot be nil",
		},
		{
			name:        "missing tracker",
			base:        base,
			observer:    observer,
			wantMessage: "tracker cannot be nil",
		},
		{
			name:        "missing observer",
			base:        base,
			tracker:     tracker,
			wantMessage: "observer cannot be nil",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewPassiveHealthTransport(
				test.base,
				test.tracker,
				test.observer,
			)
			if err == nil {
				t.Fatalf("NewPassiveHealthTransport() error = nil, want %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("NewPassiveHealthTransport() transport = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("NewPassiveHealthTransport() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestPassiveHealthTransportDelegatesToBase(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{target}, 1, time.Second)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	wantResponse := &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}
	wantError := errors.New("base transport failure")
	request := passiveHealthRequest(target)
	var gotRequest *http.Request
	base := passiveHealthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotRequest = req
		return wantResponse, wantError
	})
	transport, err := NewPassiveHealthTransport(base, tracker, noopPassiveHealthObserver{})
	if err != nil {
		t.Fatalf("NewPassiveHealthTransport() error = %v", err)
	}

	gotResponse, gotError := transport.RoundTrip(request)
	if gotResponse != nil && gotResponse.Body != nil {
		defer func() { _ = gotResponse.Body.Close() }()
	}

	if gotRequest != request {
		t.Errorf("base RoundTrip() request = %p, want %p", gotRequest, request)
	}
	if gotResponse != wantResponse {
		t.Errorf("RoundTrip() response = %p, want %p", gotResponse, wantResponse)
	}
	if !errors.Is(gotError, wantError) {
		t.Errorf("RoundTrip() error = %v, want %v", gotError, wantError)
	}
}

type passiveHealthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f passiveHealthRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type noopPassiveHealthObserver struct{}

func (noopPassiveHealthObserver) RecordEjection(url.URL) {}

type recordingPassiveHealthObserver struct {
	ejections []url.URL
}

func (o *recordingPassiveHealthObserver) RecordEjection(target url.URL) {
	o.ejections = append(o.ejections, target)
}

func passiveHealthRequest(target url.URL) *http.Request {
	ctx := proxytarget.WithTarget(context.Background(), target)
	return (&http.Request{}).WithContext(ctx)
}
