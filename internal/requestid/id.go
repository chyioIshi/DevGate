package requestid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Generate returns a cryptographically random 128-bit request ID
// encoded as lowercase hexadecimal.
func Generate() (string, error) {
	return generate(rand.Reader)
}

func generate(r io.Reader) (string, error) {
	var randomBytes [16]byte

	_, err := io.ReadFull(r, randomBytes[:])
	if err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes[:]), nil
}
