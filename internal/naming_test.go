package internal_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Package directories name what the code does. Ticket or increment numbers
// (the former captcha203, kyc233, ...) belong in commit messages and plans, not
// in import paths that every caller repeats and readers must decode.
func TestPackageDirectoriesCarryNoTicketNumbers(t *testing.T) {
	numbered := regexp.MustCompile(`[0-9]{3}$`)
	var found []string
	for _, root := range []string{".", filepath.Join("..", "cmd")} {
		e := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() != "testdata" && numbered.MatchString(d.Name()) {
				found = append(found, path)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Fatalf("rename these package directories without ticket numbers:\n%s", strings.Join(found, "\n"))
	}
}

// Go file names follow the same rule. Existing numbered files are renamed when
// their code is next changed (AGENTS.md); testdata/numbered-go-files.txt lists
// the ones that remain and may only shrink.
func TestNoNewTicketNumberedGoFiles(t *testing.T) {
	data, e := os.ReadFile(filepath.Join("testdata", "numbered-go-files.txt"))
	if e != nil {
		t.Fatal(e)
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			listed[line] = true
		}
	}
	numbered := regexp.MustCompile(`[A-Za-z_](1[0-9]{2}|2[0-4][0-9])[a-z]?(_|\.go$)`)
	migrations := filepath.Join("..", "db", "migrations")
	found := map[string]bool{}
	var added []string
	for _, root := range []string{".", filepath.Join("..", "cmd"), filepath.Join("..", "db"), filepath.Join("..", "tests")} {
		e := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "testdata" || path == migrations) {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || !numbered.MatchString(d.Name()) {
				return nil
			}
			name := filepath.ToSlash(path)
			if root == "." {
				name = "internal/" + name
			} else {
				name = strings.TrimPrefix(name, "../")
			}
			found[name] = true
			if !listed[name] {
				added = append(added, name)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	var gone []string
	for name := range listed {
		if !found[name] {
			gone = append(gone, name)
		}
	}
	sort.Strings(added)
	sort.Strings(gone)
	if len(added) > 0 {
		t.Errorf("name these new files after what the code does, without ticket numbers:\n%s", strings.Join(added, "\n"))
	}
	if len(gone) > 0 {
		t.Errorf("these numbered files were renamed or removed; delete them from internal/testdata/numbered-go-files.txt:\n%s", strings.Join(gone, "\n"))
	}
}
