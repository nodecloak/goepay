//go:build !windows

package cryptosigner

import (
	"os"
	"testing"
)

func provisionPrivateTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
func provisionBroadenTestDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0777); err != nil {
		t.Fatal(err)
	}
}
