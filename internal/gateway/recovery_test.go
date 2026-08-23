package gateway_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
)

func TestHandlerRecoversPanicBeforeResponse(t *testing.T) {
	logger, logOutput := newTestLogger()
	handler := requestid.Middleware(
		newHandlerWithRoute(t, logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		})),
		logger,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if got, want := recorder.Body.String(), http.StatusText(http.StatusInternalServerError)+"\n"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
	requestID := recorder.Header().Get(requestid.HeaderName)
	if requestID == "" {
		t.Fatal("response request ID is empty")
	}

	logs := decodeRecoveryLogs(t, logOutput)
	if len(logs) != 2 {
		t.Fatalf("log record count = %d, want 2", len(logs))
	}
	assertRecoveryLog(t, logs[0], recoveryLogRecord{
		Level:     slog.LevelError.String(),
		Message:   "panic recovered",
		Method:    http.MethodGet,
		Path:      "/api/users",
		RequestID: requestID,
		Panic:     "boom",
	})
	assertRecoveryAccessLog(t, logs[1], http.StatusInternalServerError, int64(recorder.Body.Len()))
}

func TestHandlerAbortsPanicAfterResponseStarted(t *testing.T) {
	logger, logOutput := newTestLogger()
	handler := requestid.Middleware(
		newHandlerWithRoute(t, logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("partial"))
			panic("boom")
		})),
		logger,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	recovered := serveAndRecover(handler, recorder, request)

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered panic = %v, want http.ErrAbortHandler", recovered)
	}
	if recorder.Code != http.StatusCreated {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if got, want := recorder.Body.String(), "partial"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
	requestID := recorder.Header().Get(requestid.HeaderName)
	if requestID == "" {
		t.Fatal("response request ID is empty")
	}

	logs := decodeRecoveryLogs(t, logOutput)
	if len(logs) != 2 {
		t.Fatalf("log record count = %d, want 2", len(logs))
	}
	assertRecoveryLog(t, logs[0], recoveryLogRecord{
		Level:     slog.LevelError.String(),
		Message:   "panic recovered",
		Method:    http.MethodGet,
		Path:      "/api/users",
		RequestID: requestID,
		Panic:     "boom",
	})
	assertRecoveryAccessLog(t, logs[1], http.StatusCreated, int64(len("partial")))
}

func TestHandlerPropagatesErrAbortHandler(t *testing.T) {
	logger, logOutput := newTestLogger()
	handler := newHandlerWithRoute(t, logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	recovered := serveAndRecover(handler, recorder, request)

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered panic = %v, want http.ErrAbortHandler", recovered)
	}
	logs := decodeRecoveryLogs(t, logOutput)
	if len(logs) != 1 {
		t.Fatalf("log record count = %d, want only the access log", len(logs))
	}
	if logs[0].Message != "request completed" {
		t.Errorf("log message = %q, want %q", logs[0].Message, "request completed")
	}
}

type recoveryLogRecord struct {
	Level      string   `json:"level"`
	Message    string   `json:"msg"`
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Route      string   `json:"route"`
	RequestID  string   `json:"request_id"`
	Panic      string   `json:"panic"`
	Stack      string   `json:"stack"`
	Status     int      `json:"status"`
	Bytes      int64    `json:"bytes"`
	DurationMS *float64 `json:"duration_ms"`
}

func newHandlerWithRoute(t *testing.T, logger *slog.Logger, routeHandler http.Handler) http.Handler {
	t.Helper()

	return gateway.New(
		mustNewRouter(t, []router.Route{
			{
				Name:        "api",
				Protocol:    router.ProtocolHTTP,
				PathPrefix:  "/api",
				UpstreamURL: mustParseURL(t, "http://api-service:8080"),
			},
		}),
		map[string]http.Handler{"api": routeHandler},
		logger,
		metrics.NewHTTP(),
	)
}

func serveAndRecover(handler http.Handler, w http.ResponseWriter, r *http.Request) (recovered any) {
	defer func() {
		recovered = recover()
	}()

	handler.ServeHTTP(w, r)
	return nil
}

func decodeRecoveryLogs(t *testing.T, output *bytes.Buffer) []recoveryLogRecord {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var records []recoveryLogRecord
	for {
		var record recoveryLogRecord
		err := decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode log record: %v", err)
		}
		records = append(records, record)
	}

	return records
}

func assertRecoveryLog(t *testing.T, got, want recoveryLogRecord) {
	t.Helper()

	if got.Level != want.Level {
		t.Errorf("recovery log level = %q, want %q", got.Level, want.Level)
	}
	if got.Message != want.Message {
		t.Errorf("recovery log message = %q, want %q", got.Message, want.Message)
	}
	if got.Method != want.Method {
		t.Errorf("recovery log method = %q, want %q", got.Method, want.Method)
	}
	if got.Path != want.Path {
		t.Errorf("recovery log path = %q, want %q", got.Path, want.Path)
	}
	if got.RequestID != want.RequestID {
		t.Errorf("recovery log request ID = %q, want %q", got.RequestID, want.RequestID)
	}
	if got.Panic != want.Panic {
		t.Errorf("recovery log panic = %q, want %q", got.Panic, want.Panic)
	}
	if !strings.Contains(got.Stack, "TestHandler") {
		t.Errorf("recovery log stack does not contain the panicking test handler: %q", got.Stack)
	}
}

func assertRecoveryAccessLog(t *testing.T, got recoveryLogRecord, wantStatus int, wantBytes int64) {
	t.Helper()

	if got.Message != "request completed" {
		t.Errorf("access log message = %q, want %q", got.Message, "request completed")
	}
	if got.Status != wantStatus {
		t.Errorf("access log status = %d, want %d", got.Status, wantStatus)
	}
	if got.Bytes != wantBytes {
		t.Errorf("access log bytes = %d, want %d", got.Bytes, wantBytes)
	}
	if got.DurationMS == nil {
		t.Error("access log duration_ms is missing")
	}
}
