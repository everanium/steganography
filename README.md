# Steganography — Dynamic Bit-Shifting Image Steganography

A Go library for hiding arbitrary binary data in images using dynamic bit-shifting steganography.

**3 bits per pixel** across RGB channels with per-pixel bit position randomization via a pluggable hash function, making the encoding pattern undetectable without knowledge of the seed.

**Zero external dependencies.** Hash function is supplied by the user.

## Features

- **Pluggable hash function** — user supplies any `func([]byte, uint64) uint64` (XXH3, SipHash, BLAKE3, etc.)
- **Dynamic bit position** — each pixel independently uses bit 0 or bit 1 of each RGB channel, selected by `hash(seed, x, y)`. Defeats standard steganalysis tools (e.g., zsteg, StegExpose)
- **Seed-dependent start pixel** — data embedding begins at a pseudo-random pixel offset, not at (0,0). Defeats spatial analysis
- **Seed-derived separators** — no fixed byte patterns in the bit stream. Enables fast wrong-seed rejection
- **Hash-based integrity checksum** — payload verified on decode, wrong seeds rejected immediately
- **Phase machine decoder** — zero-allocation header parsing with early exit on wrong seed
- **Works with any image format** — operates on `*image.NRGBA`; caller handles PNG/QOI/BMP encoding
- **Zero external dependencies** — hash function supplied by caller

## Steganalysis Resistance

Standard steganalysis tools (zsteg, StegExpose, chi-square analysis) rely on detecting patterns in how data is embedded into image pixels. This library is designed to defeat all known automated detection methods:

**No fixed bit positions.** Traditional LSB steganography always uses bit 0 of each channel. Analysts know exactly where to look. This library randomly alternates between bit 0 and bit 1 on every pixel, determined by `hash(seed, x, y)`. Without the seed, the analyst cannot determine which bit carries data on any given pixel.

**No spatial patterns.** Data embedding starts at a seed-dependent pixel offset and wraps around the image — not at pixel (0,0). There is no "clean region" vs "data region" boundary that spatial analysis can detect. The entire image participates uniformly.

**No fixed byte markers.** Separators and structure bytes are derived from the seed. Each seed produces different separators. There are no constant byte patterns (magic numbers, headers) that frequency analysis can find in the bit stream.

**No hot zones in LSB plane.** Since each pixel independently uses either bit 0 or bit 1, the LSB plane (bit 0 of all channels) contains a mix of data bits and original image bits. The bit-1 plane similarly contains a mix. Neither plane shows the statistical anomalies that chi-square or RS analysis depends on. The data is spread across two bit planes in an unpredictable pattern.

**Analyst's perspective without the seed:**
- Cannot determine which bit position (0 or 1) was used on each pixel
- Cannot determine where data starts in the image
- Cannot identify structure bytes in the stream
- Cannot distinguish data-carrying pixels from unmodified pixels
- Statistical analysis of any single bit plane shows a mix of data and image, not a clean signal

**What remains detectable:** steganography cannot hide the fact that an image exists and has been transmitted. If the cover image has large uniform-color regions (white background, solid fills), even randomized bit modifications may be visually or statistically noticeable. Additionally, if the original cover image is publicly available to the analyst, a direct comparison will reveal modified pixels regardless of the embedding method. Use unique, non-public natural photographs or complex images as cover for best results.

## How It Works

Each pixel stores 3 bits of data — one bit per RGB channel. The bit position (0 or 1) is determined by the hash function:

```
Pixel at (x, y), hash selects bit 0:

R = 11010101 → R = 11010100  (bit 0 set to data bit)
G = 10101110 → G = 10101111  (bit 0 set to data bit)
B = 11110011 → B = 11110010  (bit 0 set to data bit)
```

Without the seed, an observer cannot determine which bit position was used for each pixel — the pattern appears random.

## Bit Stream Layout

```
Offset   Size   Content
──────   ────   ──────────────────────────
 0       4      Payload size (uint32 BE)
 4       2      Separator (seed-derived)
 6       8      Hash checksum (uint64 BE)
14       2      Separator (seed-derived)
16       N      Payload data
16+N     16     Random EOF marker
```

## Installation

```bash
go get github.com/everanium/steganography
```

## Quick Start

```go
package main

import (
	"fmt"
	"image/png"
	"os"

	"github.com/everanium/steganography"
	"github.com/zeebo/xxh3"
)

// Hash function wrapper — any func([]byte, uint64) uint64
func xxh3Hash(data []byte, seed uint64) uint64 {
	return xxh3.HashSeed(data, seed)
}

func main() {
	// Generate a random seed (must be shared between encoder and decoder)
	seed := steganography.GenerateSeed()
	fmt.Printf("Seed: %016x\n", seed)

	// Load cover image
	f, _ := os.Open("cover.png")
	defer f.Close()
	img, _ := png.Decode(f)
	cover := steganography.ToNRGBA(img)

	// Check capacity
	fmt.Printf("Max payload: %d bytes\n", steganography.MaxPayloadBytes(cover))

	// Encode payload into image
	payload := []byte("secret message")
	encoded, err := steganography.Encode(cover, payload, seed, xxh3Hash)
	if err != nil {
		panic(err)
	}

	// Save steganographic image
	out, _ := os.Create("output.png")
	defer out.Close()
	png.Encode(out, encoded)

	// --- Decode ---

	// Load steganographic image
	f2, _ := os.Open("output.png")
	defer f2.Close()
	img2, _ := png.Decode(f2)
	stgImg := steganography.ToNRGBA(img2)

	// Decode with the same seed and hash
	decoded, err := steganography.Decode(stgImg, seed, xxh3Hash)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Decoded: %s\n", string(decoded))
}
```

## Hash Function

The library accepts any hash function with the signature:

```go
type HashFunc func(data []byte, seed uint64) uint64
```

The hash function is used for bit position selection, separator derivation, start pixel offset, and integrity checksum. Any hash with good avalanche properties is suitable:

```go
// XXH3 (fast, non-cryptographic)
func xxh3Hash(data []byte, seed uint64) uint64 {
    return xxh3.HashSeed(data, seed)
}

// SipHash-2-4 (PRF, cryptographic)
func sipHash(data []byte, seed uint64) uint64 {
    return siphash.Hash(seed, 0, data)
}
```

The same hash function and seed must be used for both encoding and decoding.

## API

```go
// HashFunc is the pluggable hash function interface.
type HashFunc func(data []byte, seed uint64) uint64

// Encode embeds payload into a cover image.
func Encode(img *image.NRGBA, payload []byte, seed uint64, hash HashFunc) (*image.NRGBA, error)

// Decode extracts payload from a steganographic image.
// Returns error on wrong seed or corrupt data.
func Decode(img *image.NRGBA, seed uint64, hash HashFunc) ([]byte, error)

// ToNRGBA converts any image.Image to *image.NRGBA.
func ToNRGBA(src image.Image) *image.NRGBA

// MaxPayloadBytes returns maximum embeddable payload size for the image.
func MaxPayloadBytes(img *image.NRGBA) int

// GenerateSeed returns a cryptographically random uint64 seed.
func GenerateSeed() uint64
```

## Image Format

The library operates on `*image.NRGBA` (non-premultiplied RGBA). Use `ToNRGBA()` to convert from any `image.Image`. Only RGB channels are used for data embedding; the alpha channel is preserved at 255.

**Important:** Use only lossless image formats (PNG, BMP, QOI) for saving steganographic images. Lossy formats destroy the embedded data.

## See Also

- [ITB](https://github.com/everanium/itb) — Information-Theoretic Barrier cipher construction (evolved from this steganography library)

## License

MIT — see [LICENSE](LICENSE).
