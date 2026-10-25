package collectionstyle

import (
	"bytes"
	"image"
	imagecolor "image/color"
	"image/png"
	"testing"
)

func validBody() map[string]any {
	return map[string]any{"name": "test_style", "label": "样式", "enabled": true, "sort": "0", "url": "/collection-style-assets/" + string(bytes.Repeat([]byte{'a'}, 64)), "img_width": "900", "img_height": "1200", "qr_width": "380", "qr_height": "380", "qr_left": "245", "qr_top": "326", "foreground": "#555555", "background": "transparent", "font": "60px '黑体'", "font_color": "transparent", "rec_name_left": "", "rec_name_top": "210", "word": "0"}
}
func TestOriginalLayoutBoundsAndLegacy(t *testing.T) {
	l, e := Parse(validBody())
	if e != nil || l.Width != 900 || l.TextLeft != nil || l.Background != "transparent" || l.Word != 0 {
		t.Fatalf("original fields %+v %v", l, e)
	}
	if l, e = Parse(map[string]any{"name": "old-style", "title": "Old", "background_color": "#ffffff", "text_color": "#000000"}); e != nil || l != nil {
		t.Fatal("old style acquired made-up geometry", l, e)
	}
	for _, tc := range []struct {
		field string
		value any
	}{{"qr_left", "899"}, {"qr_width", "0"}, {"img_height", "5000"}, {"img_width", "4096"}, {"rec_name_left", "901"}, {"word", "3"}, {"font", "60px Arial;url(https://example.test)"}, {"url", "javascript:alert(1)"}, {"url", "https://user:pass@example.test/background"}, {"font_color", "<script>"}, {"foreground", "transparent"}} {
		t.Run(tc.field+"/"+stringify(tc.value), func(t *testing.T) {
			body := validBody()
			body[tc.field] = tc.value
			if _, e := Parse(body); e == nil {
				t.Fatal("invalid layout accepted")
			}
		})
	}
}
func stringify(v any) string { s, _ := v.(string); return s }
func TestOriginalLayoutRejectsMismatchedFontQuotes(t *testing.T) {
	body := validBody()
	body["font"] = `60px '黑体"`
	if _, e := Parse(body); e == nil {
		t.Fatal("mismatched font quotes accepted")
	}
}
func TestBackgroundNormalizationLimitsAndMetadataRemoval(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 900, 1200))
	img.Set(0, 0, imagecolor.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if e := png.Encode(&b, img); e != nil {
		t.Fatal(e)
	}
	raw := append(b.Bytes(), []byte("<script>must not survive normalization</script>")...)
	a, e := NormalizeImage(raw)
	if e != nil || a.Width != 900 || a.Height != 1200 || a.MIME != "image/png" || len(a.ID) != 64 || bytes.Contains(a.Bytes, []byte("<script>")) {
		t.Fatalf("image normalization %+v %v", a, e)
	}
	for _, raw := range [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), make([]byte, MaxImageInput+1), {1, 2, 3}} {
		if _, e := NormalizeImage(raw); e == nil {
			t.Fatal("invalid image accepted")
		}
	}
	b.Reset()
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4097, 1)))
	if _, e = NormalizeImage(b.Bytes()); e == nil {
		t.Fatal("oversize image edge accepted")
	}
}
