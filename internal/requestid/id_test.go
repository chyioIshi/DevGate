package requestid

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestGenerateReturnsHexEncoded128BitID(t *testing.T) {
	got, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(got) != 32 {
		t.Errorf("Generate() length = %d, want %d", len(got), 32)
	}
	decoded, err := hex.DecodeString(got)
	if err != nil {
		t.Fatalf("decode generated request ID %q: %v", got, err)
	}
	if len(decoded) != 16 {
		t.Errorf("decoded request ID length = %d, want %d", len(decoded), 16)
	}
}

func TestGenerateEncodesReaderBytes(t *testing.T) {
	randomBytes := []byte{
		0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b,
		0x0c, 0x0d, 0x0e, 0x0f,
	}

	got, err := generate(bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	const want = "000102030405060708090a0b0c0d0e0f"
	if got != want {
		t.Errorf("generate() = %q, want %q", got, want)
	}
}

func TestGenerateReturnsReaderError(t *testing.T) {
	errEntropyUnavailable := errors.New("entropy unavailable")
	tests := []struct {
		name    string
		reader  io.Reader
		wantErr error
	}{
		{
			name:    "short read",
			reader:  bytes.NewReader(make([]byte, 15)),
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name:    "reader failure",
			reader:  iotest.ErrReader(errEntropyUnavailable),
			wantErr: errEntropyUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := generate(test.reader)
			if err == nil {
				t.Fatal("generate() error = nil, want reader error")
			}
			if got != "" {
				t.Errorf("generate() = %q, want empty ID", got)
			}
			if !errors.Is(err, test.wantErr) {
				t.Errorf("generate() error = %v, want %v", err, test.wantErr)
			}
			if !strings.Contains(err.Error(), "read random bytes") {
				t.Errorf("generate() error = %q, want read context", err)
			}
		})
	}
}
