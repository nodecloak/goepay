package pagetemplate

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

func TestCanonicalImageWriterCannotBypassOutputLimit(t *testing.T) {
	var b boundedImageBuffer
	if _, e := b.Write(make([]byte, MaxImageOutput)); e != nil {
		t.Fatal(e)
	}
	if _, e := io.WriteString(&b, "x"); e == nil || b.Len() > MaxImageOutput {
		t.Fatal("encoder string path bypassed output cap")
	}
}

func imageBytes(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{10, 80, 120, 255})
	var b bytes.Buffer
	var e error
	if format == "png" {
		e = png.Encode(&b, img)
	} else {
		e = jpeg.Encode(&b, img, nil)
	}
	if e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestNormalizeImageReencodesSupportedPixelsAndRejectsBounds(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		raw := append(imageBytes(t, format), []byte("private-metadata-must-not-survive")...)
		a, e := NormalizeImage(raw)
		if e != nil || len(a.ID) != 64 || a.Width != 4 || a.Height != 4 || bytes.Contains(a.Bytes, []byte("private-metadata")) {
			t.Fatal("image not normalized", a, e)
		}
	}
	for _, raw := range [][]byte{[]byte(`<svg onload="alert(1)"></svg>`), []byte("GIF89a"), make([]byte, MaxImageInput+1), []byte("broken")} {
		if _, e := NormalizeImage(raw); e == nil {
			t.Fatal("invalid image accepted")
		}
	}
	bomb := imageBytes(t, "png")
	binary.BigEndian.PutUint32(bomb[16:20], 999999)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	if _, e := NormalizeImage(bomb); e == nil {
		t.Fatal("oversized PNG decoded")
	}
}
