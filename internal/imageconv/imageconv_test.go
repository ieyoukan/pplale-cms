package imageconv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gen2brain/webp"
)

func TestConvertResizesToContractDimensions(t *testing.T) {
	src := encodePNG(t, synthetic(1600, 2400))

	got, err := Convert(src)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if got.Width != CardWidth || got.Height != 1200 {
		t.Errorf("card size = %dx%d, want %dx1200", got.Width, got.Height, CardWidth)
	}
	assertSize(t, "webp", got.WebP, CardWidth, 1200)
	assertSize(t, "og png", got.OGPNG, OGWidth, 360)
	if got.SourceType != "png" {
		t.Errorf("SourceType = %q, want png", got.SourceType)
	}
	if got.SourceBytes != len(src) {
		t.Errorf("SourceBytes = %d, want %d", got.SourceBytes, len(src))
	}
}

func TestConvertRoundsHeightLikeSharp(t *testing.T) {
	// 1000x333 scaled to width 800 gives 266.4 -> sharp rounds to 266.
	got, err := Convert(encodePNG(t, synthetic(1000, 333)))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.Height != 266 {
		t.Errorf("height = %d, want 266", got.Height)
	}
}

func TestConvertNeverUpscales(t *testing.T) {
	got, err := Convert(encodePNG(t, synthetic(120, 180)))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.Width != 120 || got.Height != 180 {
		t.Errorf("card size = %dx%d, want the original 120x180", got.Width, got.Height)
	}
	assertSize(t, "webp", got.WebP, 120, 180)
	assertSize(t, "og png", got.OGPNG, 120, 180)
}

func TestConvertAcceptsWebPInput(t *testing.T) {
	var buf bytes.Buffer
	if err := webp.Encode(&buf, synthetic(1600, 2400), webp.Options{Quality: 90}); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}

	got, err := Convert(buf.Bytes())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.SourceType != "webp" {
		t.Errorf("SourceType = %q, want webp", got.SourceType)
	}
	assertSize(t, "webp", got.WebP, CardWidth, 1200)
}

func TestConvertRejectsBadInput(t *testing.T) {
	cases := map[string][]byte{
		"empty":     nil,
		"not image": []byte("<html>definitely not an image</html>"),
		"truncated": encodePNG(t, synthetic(64, 64))[:20],
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Convert(src); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestConvertRejectsOversizedUpload(t *testing.T) {
	_, err := Convert(make([]byte, MaxSourceBytes+1))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// A decompression bomb is a tiny file declaring enormous dimensions. The guard
// must reject it from the header alone, before any pixel buffer is allocated.
func TestConvertRejectsDecompressionBomb(t *testing.T) {
	_, err := Convert(pngHeaderOnly(30000, 30000))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func assertSize(t *testing.T, label string, data []byte, wantW, wantH int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode %s: %v", label, err)
	}
	if cfg.Width != wantW || cfg.Height != wantH {
		t.Errorf("%s = %dx%d, want %dx%d", label, cfg.Width, cfg.Height, wantW, wantH)
	}
}

func synthetic(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func pngHeaderOnly(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	binary.Write(&ihdr, binary.BigEndian, w)
	binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0})

	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	binary.Write(&out, binary.BigEndian, uint32(13))
	out.Write(ihdr.Bytes())
	binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}
