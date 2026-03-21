package steganography

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"testing"
)

// makeTestImage creates an NRGBA image with random pixel data.
func makeTestImage(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	rand.Read(img.Pix)
	// ensure alpha = 255 for all pixels
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

// copyImage returns a deep copy of an NRGBA image.
func copyImage(img *image.NRGBA) *image.NRGBA {
	cp := image.NewNRGBA(img.Bounds())
	copy(cp.Pix, img.Pix)
	return cp
}

func TestRoundtrip(t *testing.T) {
	sizes := []int{1, 10, 64, 256, 1024}
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%d-bytes", sz), func(t *testing.T) {
			seed := GenerateSeed()
			payload := make([]byte, sz)
			rand.Read(payload)

			img := makeTestImage(128, 128)
			encoded, err := Encode(copyImage(img), payload, seed)
			if err != nil {
				t.Fatal(err)
			}

			decoded, err := Decode(encoded, seed)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(payload, decoded) {
				t.Fatalf("payload mismatch: got %d bytes, want %d bytes", len(decoded), len(payload))
			}
		})
	}
}

func TestWrongSeed(t *testing.T) {
	seed1 := GenerateSeed()
	seed2 := GenerateSeed()

	payload := []byte("secret message")
	img := makeTestImage(64, 64)

	encoded, err := Encode(img, payload, seed1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decode(encoded, seed2)
	if err == nil {
		t.Fatal("expected error for wrong seed, got nil")
	}
}

func TestEmptyPayload(t *testing.T) {
	seed := GenerateSeed()
	img := makeTestImage(64, 64)

	_, err := Encode(img, []byte{}, seed)
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestPayloadTooLarge(t *testing.T) {
	seed := GenerateSeed()
	img := makeTestImage(8, 8) // very small image

	payload := make([]byte, 1024) // too large for 8x8
	rand.Read(payload)

	_, err := Encode(img, payload, seed)
	if err == nil {
		t.Fatal("expected error for oversized payload")
	}
}

func TestMaxPayloadBytes(t *testing.T) {
	img := makeTestImage(128, 128)
	maxBytes := MaxPayloadBytes(img)

	if maxBytes <= 0 {
		t.Fatalf("MaxPayloadBytes should be positive, got %d", maxBytes)
	}

	// should fit exactly max bytes
	payload := make([]byte, maxBytes)
	rand.Read(payload)

	seed := GenerateSeed()
	encoded, err := Encode(copyImage(img), payload, seed)
	if err != nil {
		t.Fatalf("payload at max capacity should fit: %v", err)
	}

	decoded, err := Decode(encoded, seed)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !bytes.Equal(payload, decoded) {
		t.Fatal("payload mismatch at max capacity")
	}
}

func TestToNRGBA(t *testing.T) {
	// create a regular RGBA image
	rgba := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			rgba.Set(x, y, color.RGBA{uint8(x * 16), uint8(y * 16), 128, 255})
		}
	}

	nrgba := ToNRGBA(rgba)
	if nrgba.Bounds() != rgba.Bounds() {
		t.Fatal("bounds mismatch after ToNRGBA")
	}
}

func TestBinaryData(t *testing.T) {
	// payload with all byte values including 0x00
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}

	seed := GenerateSeed()
	img := makeTestImage(128, 128)

	encoded, err := Encode(img, payload, seed)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded, seed)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(payload, decoded) {
		t.Fatal("binary data roundtrip failed")
	}
}

func TestDifferentSeedsDifferentOutput(t *testing.T) {
	payload := []byte("same payload")
	seed1 := GenerateSeed()
	seed2 := GenerateSeed()

	img1 := makeTestImage(64, 64)
	img2 := copyImage(img1)

	enc1, err := Encode(img1, payload, seed1)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := Encode(img2, payload, seed2)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(enc1.Pix, enc2.Pix) {
		t.Fatal("different seeds should produce different encoded images")
	}
}

func BenchmarkEncode(b *testing.B) {
	seed := GenerateSeed()
	payload := make([]byte, 1024)
	rand.Read(payload)
	img := makeTestImage(128, 128)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Encode(copyImage(img), payload, seed)
	}
}

func BenchmarkDecode(b *testing.B) {
	seed := GenerateSeed()
	payload := make([]byte, 1024)
	rand.Read(payload)
	img := makeTestImage(128, 128)
	encoded, _ := Encode(img, payload, seed)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Decode(encoded, seed)
	}
}
