//go:build !windows

package cryptosigner

import (
	"fmt"
	"os"
)

func provisionPrivatePermissions(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("key directory and files must be owner-only (0700 directories, 0600 files)")
	}
	return nil
}
func provisionSyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
