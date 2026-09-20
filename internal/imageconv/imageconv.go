// Package imageconv converts an uploaded card illustration into the two
// artefacts PPLALE-web expects: the 800px WebP served by the site and the
// 240px PNG mirror the OGP renderer reads. Both must land in the same pull
// request; upstream CI only checks the former.
package imageconv

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"

	xdraw "golang.org/x/image/draw"

	"github.com/gen2brain/webp"
)

const (
	// CardWidth and Quality mirror PPLALE-web's scripts/optimize-images.mjs.
	CardWidth = 800
	Quality   = 80
	// OGWidth mirrors scripts/generate-og-images.mjs.
	OGWidth = 240

	// MaxSourceBytes caps an upload before it is handed to a decoder.
	MaxSourceBytes = 20 << 20
	// MaxSourcePixels rejects decompression bombs: a small file that claims
	// enormous dimensions would otherwise allocate gigabytes.
	MaxSourcePixels = 40_000_000
)

// ErrTooLarge is returned for uploads that exceed the size or pixel caps.
var ErrTooLarge = errors.New("imageconv: 画像が大きすぎます")

// Result holds both rendered artefacts plus the numbers the pull request body
// reports back to the submitter.
type Result struct {
	WebP        []byte
	OGPNG       []byte
	SourceBytes int
	SourceType  string
	Width       int
	Height      int
}

// Convert decodes an uploaded PNG, JPEG or WebP and renders both artefacts.
// Images narrower than the targets are never upscaled, matching sharp's
// withoutEnlargement.
func Convert(src []byte) (Result, error) {
	if len(src) == 0 {
		return Result{}, errors.New("imageconv: 画像データが空です")
	}
	if len(src) > MaxSourceBytes {
		return Result{}, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(src))
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return Result{}, fmt.Errorf("imageconv: 対応していない画像形式です: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Result{}, errors.New("imageconv: 画像サイズが不正です")
	}
	if cfg.Width*cfg.Height > MaxSourcePixels {
		return Result{}, fmt.Errorf("%w: %dx%d", ErrTooLarge, cfg.Width, cfg.Height)
	}

	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return Result{}, fmt.Errorf("imageconv: 画像を読み込めませんでした: %w", err)
	}

	card := resizeToWidth(img, CardWidth)
	var webpBuf bytes.Buffer
	if err := webp.Encode(&webpBuf, card, webp.Options{Quality: Quality}); err != nil {
		return Result{}, fmt.Errorf("imageconv: WebP 変換に失敗しました: %w", err)
	}

	og := resizeToWidth(card, OGWidth)
	var pngBuf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&pngBuf, og); err != nil {
		return Result{}, fmt.Errorf("imageconv: OGP用 PNG 変換に失敗しました: %w", err)
	}

	b := card.Bounds()
	return Result{
		WebP:        webpBuf.Bytes(),
		OGPNG:       pngBuf.Bytes(),
		SourceBytes: len(src),
		SourceType:  format,
		Width:       b.Dx(),
		Height:      b.Dy(),
	}, nil
}

// resizeToWidth scales an image down to width px, preserving aspect ratio and
// never enlarging.
func resizeToWidth(img image.Image, width int) image.Image {
	b := img.Bounds()
	if b.Dx() <= width {
		return toRGBA(img)
	}
	height := int(math.Round(float64(b.Dy()) * float64(width) / float64(b.Dx())))
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

func toRGBA(img image.Image) image.Image {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}
