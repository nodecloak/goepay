package pagetemplate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"image/png"
)

const MaxImageInput = 512 << 10
const MaxImageOutput = 2 << 20

type Asset struct {
	ID     string `json:"id"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  []byte `json:"-"`
}
type boundedImageBuffer struct{ buffer bytes.Buffer }

func (b *boundedImageBuffer) Len() int      { return b.buffer.Len() }
func (b *boundedImageBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *boundedImageBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxImageOutput-b.Len() {
		return 0, ErrInvalid
	}
	return b.buffer.Write(p)
}
func NormalizeImage(raw []byte) (Asset, error) {
	var a Asset
	if len(raw) == 0 || len(raw) > MaxImageInput {
		return a, ErrInvalid
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 || int64(cfg.Width)*int64(cfg.Height) > 1048576 {
		return a, ErrInvalid
	}
	img, decoded, e := image.Decode(bytes.NewReader(raw))
	if e != nil || decoded != format || img.Bounds().Dx() != cfg.Width || img.Bounds().Dy() != cfg.Height {
		return a, ErrInvalid
	}
	var b boundedImageBuffer
	if format == "png" {
		e = png.Encode(&b, img)
		a.MIME = "image/png"
	} else {
		e = jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
		a.MIME = "image/jpeg"
	}
	if e != nil {
		return Asset{}, ErrInvalid
	}
	digest := sha256.Sum256(b.Bytes())
	a.ID = hex.EncodeToString(digest[:])
	a.Width = cfg.Width
	a.Height = cfg.Height
	a.Bytes = b.Bytes()
	return a, nil
}
