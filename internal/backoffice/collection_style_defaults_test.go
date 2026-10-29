package backoffice

import (
	"context"
	"testing"

	"goepay/internal/collectionstyle"
)

// A8-D2: the seeder fills an empty install with the original out-of-box styles,
// is idempotent, never seeds an install that already has styles, and every
// seeded style parses as a valid layout the generator/renderer accept.
func TestEnsureDefaultCollectionStylesSeedsOriginalSetOnce(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	n, e := s.EnsureDefaultCollectionStyles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if n != len(defaultCollectionStyles) || n == 0 {
		t.Fatalf("seeded %d styles, want %d", n, len(defaultCollectionStyles))
	}

	// A second call is a no-op: the resource is no longer empty.
	n2, e := s.EnsureDefaultCollectionStyles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if n2 != 0 {
		t.Fatalf("re-seed inserted %d styles, want 0", n2)
	}

	// Every seeded style is visible to both admin and merchant and is a valid
	// parsed layout (enabled, named, with a usable background and geometry).
	styles, e := s.CollectionStyles(ctx, Principal{Role: "admin", SubjectID: 1})
	if e != nil {
		t.Fatal(e)
	}
	if len(styles) != len(defaultCollectionStyles) {
		t.Fatalf("listed %d styles, want %d", len(styles), len(defaultCollectionStyles))
	}
	byName := map[string]*collectionstyle.Layout{}
	for _, st := range styles {
		if st.Style == nil {
			t.Fatalf("style %s did not parse as a layout", st.ID)
		}
		if !st.Style.Enabled {
			t.Fatalf("seeded style %s is not enabled", st.Style.Name)
		}
		byName[st.Style.Name] = st.Style
	}

	// Spot-check the 冬雪 (dongxue) geometry against the original pre_onecode row.
	dongxue := byName["dongxue"]
	if dongxue == nil {
		t.Fatal("dongxue style missing")
	}
	if dongxue.Label != "冬雪" || dongxue.QRWidth != 364 || dongxue.QRHeight != 364 ||
		dongxue.Width != 900 || dongxue.Height != 1200 || dongxue.QRLeft != 270 ||
		dongxue.QRTop != 340 || dongxue.Foreground != "#E3D6C6" || dongxue.Word != 2 {
		t.Fatalf("dongxue geometry drifted from the original: %+v", dongxue)
	}

	// The full original set of 18 is seeded (15 https + 3 asset-backed).
	if len(defaultCollectionStyles) != 18 {
		t.Fatalf("expected 18 original styles, have %d", len(defaultCollectionStyles))
	}
	// default / alipay / wxpay carry an embedded background seeded as an
	// uploaded asset, so their url is /collection-style-assets/<id> and the
	// asset row exists and reads back.
	assetPrefix := "/collection-style-assets/"
	for _, name := range []string{"default", "alipay", "wxpay"} {
		st := byName[name]
		if st == nil {
			t.Fatalf("asset-backed style %s missing", name)
		}
		if len(st.URL) != len(assetPrefix)+64 || st.URL[:len(assetPrefix)] != assetPrefix {
			t.Fatalf("style %s url is not an uploaded asset: %q", name, st.URL)
		}
		id := st.URL[len(assetPrefix):]
		a, e := collectionstyle.ReadAsset(ctx, s.pool, id, true)
		if e != nil || a.ID != id {
			t.Fatalf("seeded asset for %s unreadable: %v", name, e)
		}
	}

	// Merchants see the same enabled out-of-box set.
	mstyles, e := s.CollectionStyles(ctx, Principal{Role: "merchant", SubjectID: 101})
	if e != nil {
		t.Fatal(e)
	}
	if len(mstyles) != len(defaultCollectionStyles) {
		t.Fatalf("merchant listed %d styles, want %d", len(mstyles), len(defaultCollectionStyles))
	}
}

// An install that already has a style is never seeded over.
func TestEnsureDefaultCollectionStylesSkipsNonEmptyInstall(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, e := s.Create(ctx, Principal{Role: "admin", SubjectID: 1, Username: "admin"}, "collection-styles", input(`{"name":"custom","label":"自定义","enabled":true,"sort":"0","url":"https://img.alicdn.com/x.png","img_width":"900","img_height":"1200","qr_width":"380","qr_height":"380","qr_left":"245","qr_top":"326","foreground":"#555555","background":"#FFFFFF","font":"60px '黑体'","font_color":"#FFFFFF","rec_name_left":"","rec_name_top":"210","word":"2"}`)); e != nil {
		t.Fatal(e)
	}
	n, e := s.EnsureDefaultCollectionStyles(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if n != 0 {
		t.Fatalf("seeded %d styles over a non-empty install, want 0", n)
	}
}
