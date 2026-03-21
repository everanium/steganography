package steganography

import (
	"crypto/rand"
	"encoding/binary"
)

// GenerateSeed returns a cryptographically random uint64 seed.
func GenerateSeed() uint64 {
	var buf [8]byte
	rand.Read(buf[:])
	return binary.LittleEndian.Uint64(buf[:])
}
