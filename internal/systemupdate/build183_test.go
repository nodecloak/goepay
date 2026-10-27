package systemupdate

import (
	"context"
	"strings"
	"testing"
)

func Test183VersionWithoutActualSourceIdentityCannotPrepare(t *testing.T) {
	for _, source := range []string{"", "not-a-source-checkpoint"} {
		t.Run(source, func(t *testing.T) {
			s, b, _, _, _ := fixture183(t)
			b.SourceSHA = source
			view, e := s.Inspect(context.Background(), b)
			if e != nil || view.CanPrepare || view.CheckStatus != "build_unversioned" {
				t.Fatal("version label without linked source identity became actionable", view, e)
			}
		})
	}
}
func Test183SameVersionDifferentSourceDoesNotClaimCurrentRelease(t *testing.T) {
	s, b, m, key, cfg := fixture183(t)
	m.Version = b.Version
	m.SourceSHA = strings.Repeat("b", 64)
	writeManifest183(t, cfg, key, m)
	view, e := s.Inspect(t.Context(), b)
	if e != nil || view.NeedUpdate || view.CanPrepare || view.CheckStatus != "source_mismatch" {
		t.Fatal("equal version label hid different running source", view, e)
	}
}
