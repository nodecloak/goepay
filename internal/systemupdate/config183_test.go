package systemupdate

import (
	"strings"
	"testing"
)

func Test183DeploymentConfigurationIsExplicitAndNeverPartiallyEnabled(t *testing.T) {
	empty := func(string) string { return "" }
	if s, e := FromEnvironment(empty); e != nil || s == nil {
		t.Fatal(s, e)
	}
	values := map[string]string{"GOEPAY_UPDATE_MANIFEST_FILE": t.TempDir() + "/manifest.json"}
	get := func(k string) string { return values[k] }
	if _, e := FromEnvironment(get); e == nil {
		t.Fatal("partial deployment trust became enabled")
	}
	values["GOEPAY_UPDATE_PACKAGE_FILE"] = t.TempDir() + "/update.zip"
	values["GOEPAY_UPDATE_STAGING_DIR"] = t.TempDir() + "/staging"
	values["GOEPAY_UPDATE_PUBLIC_KEY"] = strings.Repeat("ab", 32)
	if _, e := FromEnvironment(get); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"", strings.Repeat("ab", 31), " " + strings.Repeat("ab", 32), strings.Repeat("zz", 32)} {
		values["GOEPAY_UPDATE_PUBLIC_KEY"] = bad
		if _, e := FromEnvironment(get); e == nil {
			t.Fatal("malformed trusted key accepted")
		}
	}
}
