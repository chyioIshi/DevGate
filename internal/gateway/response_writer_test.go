package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewResponseWriter(t *testing.T) {
	underlying := httptest.NewRecorder()

	w := newResponseWriter(underlying)

	if w.Unwrap() != underlying {
		t.Errorf("Unwrap() = %T, want original ResponseWriter", w.Unwrap())
	}
	if w.statusCode != http.StatusOK {
		t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusOK)
	}
	if w.bytesWritten != 0 {
		t.Errorf("bytesWritten = %d, want 0", w.bytesWritten)
	}
	if w.wroteHeader {
		t.Error("wroteHeader = true, want false")
	}
}

func TestResponseWriterRecordsFirstStatusCode(t *testing.T) {
	underlying := httptest.NewRecorder()
	w := newResponseWriter(underlying)

	w.WriteHeader(http.StatusCreated)
	w.WriteHeader(http.StatusInternalServerError)

	if w.statusCode != http.StatusCreated {
		t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusCreated)
	}
	if !w.wroteHeader {
		t.Error("wroteHeader = false, want true")
	}
	if underlying.Code != http.StatusCreated {
		t.Errorf("underlying status code = %d, want %d", underlying.Code, http.StatusCreated)
	}
}

func TestResponseWriterRecordsImplicitStatusAndBytes(t *testing.T) {
	underlying := httptest.NewRecorder()
	w := newResponseWriter(underlying)

	firstN, firstErr := w.Write([]byte("hello"))
	if firstErr != nil {
		t.Fatalf("first Write() error = %v", firstErr)
	}
	secondN, secondErr := w.Write([]byte("!"))
	if secondErr != nil {
		t.Fatalf("second Write() error = %v", secondErr)
	}

	if firstN != 5 || secondN != 1 {
		t.Errorf("Write() byte counts = %d, %d, want 5, 1", firstN, secondN)
	}
	if w.statusCode != http.StatusOK {
		t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusOK)
	}
	if !w.wroteHeader {
		t.Error("wroteHeader = false, want true")
	}
	if w.bytesWritten != 6 {
		t.Errorf("bytesWritten = %d, want 6", w.bytesWritten)
	}
	if got := underlying.Body.String(); got != "hello!" {
		t.Errorf("underlying body = %q, want %q", got, "hello!")
	}
}

func TestResponseWriterRecordsPartialWrite(t *testing.T) {
	wantErr := errors.New("partial write")
	underlying := &partialResponseWriter{
		header:   make(http.Header),
		written:  2,
		writeErr: wantErr,
	}
	w := newResponseWriter(underlying)

	n, err := w.Write([]byte("hello"))

	if n != 2 {
		t.Errorf("Write() n = %d, want 2", n)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Write() error = %v, want %v", err, wantErr)
	}
	if w.bytesWritten != 2 {
		t.Errorf("bytesWritten = %d, want 2", w.bytesWritten)
	}
	if w.statusCode != http.StatusOK {
		t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusOK)
	}
	if underlying.statusCode != http.StatusOK {
		t.Errorf("underlying status code = %d, want %d", underlying.statusCode, http.StatusOK)
	}
}

func TestResponseWriterUnwrapsForResponseController(t *testing.T) {
	underlying := &flushResponseWriter{header: make(http.Header)}
	w := newResponseWriter(underlying)

	err := http.NewResponseController(w).Flush()

	if err != nil {
		t.Fatalf("ResponseController.Flush() error = %v", err)
	}
	if !underlying.flushed {
		t.Error("underlying ResponseWriter was not flushed")
	}
}

type partialResponseWriter struct {
	header     http.Header
	statusCode int
	written    int
	writeErr   error
}

func (w *partialResponseWriter) Header() http.Header {
	return w.header
}

func (w *partialResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *partialResponseWriter) Write([]byte) (int, error) {
	return w.written, w.writeErr
}

type flushResponseWriter struct {
	header  http.Header
	flushed bool
}

func (w *flushResponseWriter) Header() http.Header {
	return w.header
}

func (*flushResponseWriter) WriteHeader(int) {}

func (*flushResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *flushResponseWriter) Flush() {
	w.flushed = true
}
