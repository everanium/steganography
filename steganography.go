// Package steganography implements dynamic bit-shifting steganography.
//
// Encoding 3 bits per pixel using RGB channels. Per-pixel bit position
// (bit 0 or bit 1) is dynamically selected using XXH3 hash of
// (seed, x, y) coordinates, making the encoding pattern undetectable
// by standard steganalysis tools.
//
// Features:
//   - Dynamic bit position selection per pixel via XXH3(seed, x, y)
//   - Seed-dependent start pixel offset (data doesn't start at pixel 0,0)
//   - Seed-derived separators (no fixed byte patterns in the stream)
//   - XXH3 checksum for integrity verification
//   - Fast wrong-seed rejection via separator mismatch
//
// The library operates on *image.NRGBA images. The caller is responsible
// for loading/saving images in any format (PNG, QOI, etc.).
package steganography

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/big"

	"github.com/zeebo/xxh3"
)

// useSecondBit decides which bit position (0 or 1) to use for this pixel,
// based on XXH3 hash of seed + coordinates. Defeats steganalysis by making
// the encoding pattern unpredictable without knowledge of the seed.
//
// x and y are encoded as separate fields in a fixed-size buffer to avoid
// diagonal collision where (x=3,y=5) and (x=5,y=3) would produce the same
// hash if only x+y were hashed. Zero-allocation: uses stack-allocated buffer.
func useSecondBit(seed uint64, x, y int) bool {
	var buf [4]byte
	buf[0] = byte(x)
	buf[1] = byte(x >> 8)
	buf[2] = byte(y)
	buf[3] = byte(y >> 8)
	return xxh3.HashSeed(buf[:], seed)&1 == 1
}

// makeSeparator derives a 2-byte separator from the seed.
// Each seed produces a unique separator, which serves two purposes:
// 1. Eliminates fixed byte patterns from the bit stream
// 2. Enables fast rejection of wrong seeds during brute-force decode
func makeSeparator(seed uint64) [2]byte {
	h := xxh3.HashSeed([]byte{0x01}, seed)
	s0 := byte(h)
	s1 := byte(h >> 8)
	if s0 == 0 {
		s0 = 0x7C
	}
	if s1 == 0 {
		s1 = 0x7C
	}
	return [2]byte{s0, s1}
}

// deriveStartPixel computes a seed-dependent pixel offset for data placement.
// Instead of always starting from pixel (0,0), encoding/decoding begins at
// a pseudo-random position and wraps around the image.
func deriveStartPixel(seed uint64, totalPixels int) int {
	h := xxh3.HashSeed([]byte{0x02}, seed)
	return int(h % uint64(totalPixels))
}

// maxBits returns the maximum number of bits that can be encoded in the image.
func maxBits(img *image.NRGBA) int {
	return img.Bounds().Dx() * img.Bounds().Dy() * 3
}

// generateRandomBytes returns n cryptographically random bytes.
func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// generateRandomIntN returns a cryptographically random int in [min, max).
func generateRandomIntN(min, max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	return int(n.Int64()) + min
}

// Bit stream layout (produced by Encode, consumed by Decode):
//
//	Byte offset  Content
//	──────────── ──────────────────────────
//	 0 ..  3     payload size  (uint32 BE)
//	 4 ..  5     separator     (seed-derived)
//	 6 .. 13     XXH3 checksum (uint64 BE) — xxh3(payload, seed)
//	14 .. 15     separator     (seed-derived)
//	16 .. 16+N-1 payload data  (N = size)
//	16+N ..      random padding + random EOF marker (16 bytes)
//
// Header total: 16 bytes. Decode verifies checksum over data[16 : 16+size].

// Encode embeds payload into the provided cover image using dynamic
// bit-shifting steganography. The image is modified in-place and also returned.
//
// The cover image must be *image.NRGBA. Data is embedded into the least
// significant bits of RGB channels (3 bits per pixel). The alpha channel
// is preserved unchanged.
//
// seed determines the bit position pattern, start pixel offset, and separators.
// The same seed must be used for decoding.
func Encode(img *image.NRGBA, payload []byte, seed uint64) (*image.NRGBA, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("steganography: empty payload")
	}

	// random EOF marker
	eofMarker := generateRandomBytes(16)

	// derive seed-dependent separator
	hs := makeSeparator(seed)

	// calculate checksum from payload
	xxcs := xxh3.HashSeed(payload, seed)

	// build stream: [size(4)][sep(2)][checksum(8)][sep(2)][payload][eof(16)]
	streamLen := 4 + 2 + 8 + 2 + len(payload) + 16
	stream := make([]byte, 0, streamLen)

	// size + separator
	sizeBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(sizeBuf, uint32(len(payload)))
	stream = append(stream, sizeBuf...)
	stream = append(stream, hs[0], hs[1])

	// checksum + separator
	csumBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(csumBuf, xxcs)
	stream = append(stream, csumBuf...)
	stream = append(stream, hs[0], hs[1])

	// payload + EOF marker
	stream = append(stream, payload...)
	stream = append(stream, eofMarker...)

	// check if data fits in image
	maxLen := maxBits(img)
	if len(stream)*8 > maxLen {
		return nil, fmt.Errorf("steganography: payload too large: need %d bits, image has %d bits", len(stream)*8, maxLen)
	}

	// seed-dependent start pixel offset
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	totalPixels := width * height
	startPixel := deriveStartPixel(seed, totalPixels)

	bitIndex := 0

Main:
	for p := 0; p < totalPixels; p++ {
		linearIdx := (startPixel + p) % totalPixels
		x := linearIdx % width
		y := linearIdx / width

		c := img.NRGBAAt(x, y)
		r, g, b := c.R, c.G, c.B

		// decide which bit position to use for this pixel (bit 0 or bit 1)
		use2LSB := useSecondBit(seed, x, y)

		var mask uint8
		if use2LSB {
			mask = 0b11111101
		} else {
			mask = 0b11111110
		}

		for i := 0; i < 3; i++ {
			if bitIndex >= len(stream)*8 {
				break Main
			}

			byteIndex := bitIndex / 8
			bitPos := bitIndex % 8

			bit := (stream[byteIndex] >> bitPos) & 1
			if use2LSB {
				bit = bit << 1
			}

			bitIndex++

			switch i {
			case 0:
				r = (r & mask) | bit
			case 1:
				g = (g & mask) | bit
			case 2:
				b = (b & mask) | bit
			}
		}

		img.Set(x, y, color.NRGBA{r, g, b, 255})
	}

	if bitIndex < len(stream)*8 {
		return nil, fmt.Errorf("steganography: image too small: encoded %d of %d bits", bitIndex, len(stream)*8)
	}

	return img, nil
}

// Decode extracts payload from a steganographic image using the given seed.
//
// Returns the original payload or an error if the seed is wrong or data
// is corrupt. Wrong seeds are rejected quickly via separator mismatch.
func Decode(img *image.NRGBA, seed uint64) ([]byte, error) {
	// derive seed-dependent separator
	hs := makeSeparator(seed)

	// pre-allocate data buffer to maximum possible size
	maxBytes := (img.Bounds().Dx()*img.Bounds().Dy()*3 + 7) / 8
	if maxBytes < 32 {
		return nil, fmt.Errorf("steganography: image too small for payload")
	}
	data := make([]byte, maxBytes)

	// header buffers
	var ba [6]byte  // size(4) + separator(2)
	var bb [16]byte // checksum(8) + separator(2) [offset 6..15]

	var size uint32
	var csum uint64

	// seed-dependent start pixel offset
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	totalPixels := width * height
	startPixel := deriveStartPixel(seed, totalPixels)

	bitIndex := 0

	// Phase machine:
	//   phase 0: reading size + separator into ba (bytes 0-5)
	//   phase 1: reading checksum + separator into bb (bytes 6-15)
	//   phase 2: reading payload data, verifying XXH3 checksum
	phase := 0
	cnt := 0

Main:
	for p := 0; p < totalPixels; p++ {
		linearIdx := (startPixel + p) % totalPixels
		x := linearIdx % width
		y := linearIdx / width

		c := img.NRGBAAt(x, y)
		r, g, b := c.R, c.G, c.B

		use2LSB := useSecondBit(seed, x, y)

		for i := 0; i < 3; i++ {
			byteIndex := bitIndex / 8

			if byteIndex >= maxBytes {
				break Main
			}

			// payload completion check (phase 2 only)
			if phase == 2 && byteIndex >= 16+int(size) {
				cdata := data[16 : 16+size]
				xxcs := xxh3.HashSeed(cdata, seed)
				if xxcs == csum {
					result := make([]byte, size)
					copy(result, cdata)
					return result, nil
				}
			}

			// extract bit from color channel
			var channelValue byte
			switch i {
			case 0:
				channelValue = r
			case 1:
				channelValue = g
			case 2:
				channelValue = b
			}

			if use2LSB {
				channelValue = channelValue >> 1
			}

			bit := channelValue & 1

			// route bit to appropriate buffer based on phase
			switch phase {
			case 0:
				cnt++
				if cnt > 48 {
					return nil, fmt.Errorf("steganography: separator not found (wrong seed)")
				}
				if byteIndex < 6 {
					ba[byteIndex] |= bit << (bitIndex % 8)
				}
				if byteIndex >= 5 && ba[4] == hs[0] && ba[5] == hs[1] {
					size = binary.BigEndian.Uint32(ba[0:4])
					phase = 1
				}

			case 1:
				cnt++
				if cnt > 128 {
					return nil, fmt.Errorf("steganography: checksum separator not found (wrong seed)")
				}
				if byteIndex < 16 {
					bb[byteIndex] |= bit << (bitIndex % 8)
				}
				if byteIndex >= 15 && bb[14] == hs[0] && bb[15] == hs[1] {
					csum = binary.BigEndian.Uint64(bb[6:14])
					phase = 2
				}

			case 2:
				data[byteIndex] |= bit << (bitIndex % 8)
			}

			bitIndex++
		}
	}

	return nil, fmt.Errorf("steganography: checksum mismatch (wrong seed or corrupt data)")
}

// ToNRGBA converts any image.Image to *image.NRGBA.
// This is a convenience function for loading cover images from any format.
func ToNRGBA(src image.Image) *image.NRGBA {
	bounds := src.Bounds()
	img := image.NewNRGBA(bounds)
	draw.Draw(img, bounds, src, bounds.Min, draw.Src)
	return img
}

// MaxPayloadBytes returns the maximum payload size in bytes
// that can be embedded in the given image.
func MaxPayloadBytes(img *image.NRGBA) int {
	totalBits := maxBits(img)
	headerBits := 16 * 8  // 16-byte header
	eofBits := 16 * 8     // 16-byte EOF marker
	availBits := totalBits - headerBits - eofBits
	if availBits < 0 {
		return 0
	}
	return availBits / 8
}
