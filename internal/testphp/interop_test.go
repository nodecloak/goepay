package testphp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyExplicitPHPFlagsEnableRuntime(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE"} {
		if !enabled(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"", "0", "false", "yes"} {
		if enabled(value) {
			t.Fatal(value)
		}
	}
}
func TestCasePathsRejectEscapeAndCLIOverrides(t *testing.T) {
	file := filepath.Join(t.TempDir(), "script.php")
	if err := os.WriteFile(file, []byte("<?php echo 1;"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"/etc/passwd", "/work/../../etc/passwd", "/work\\escape", "//work/test", "/work/a\x00b"} {
		if _, err := buildRequest(Spec{Mounts: []Mount{{file, destination}}, Args: []string{destination}}, 1000); err == nil {
			t.Fatal(destination)
		}
	}
	if _, err := buildRequest(Spec{Mounts: []Mount{{file, "/work/run.php"}}, Args: []string{"-d", "allow_url_fopen=1"}}, 1000); err == nil {
		t.Fatal("PHP ini override accepted")
	}
	r, err := buildRequest(Spec{Mounts: []Mount{{file, "/work/run.php"}}, Args: []string{"/work/run.php"}, Input: []byte(`{"original":"bytes"}`)}, 1000)
	if err != nil || len(r.Files) != 1 || r.Files[0].SHA != sum([]byte("<?php echo 1;")) || string(r.Input) != `{"original":"bytes"}` {
		t.Fatal(r, err)
	}
}
func TestLauncherNeverCreatesContainersOrUsesAShell(t *testing.T) {
	c := configuration{receipt: receipt{Runner: runnerName, RuntimeRoot: "/tmp/goepay-phpinterop-" + strings.Repeat("a", 32), StateSHA: strings.Repeat("b", 64), ExecutorSHA: strings.Repeat("c", 64)}}
	args := commandArgs(c)
	if len(args) != 9 || args[0] != "exec" || args[1] != "-i" || args[2] != runnerName || args[3] != "node" || args[4] != "-e" {
		t.Fatal(args)
	}
	for _, arg := range args {
		if arg == "run" || arg == "create" || arg == "pull" || arg == "sh" || arg == "bash" {
			t.Fatal("forbidden launcher command", arg)
		}
	}
}
func TestEnabledWithoutVerifiedRuntimeHasExplicitPrerequisiteError(t *testing.T) {
	t.Setenv("GOEPAY_PHP_RUNTIME_RECEIPT", "")
	t.Setenv("GOEPAY_PHP_RUNTIME_RECEIPT_SHA256", "")
	t.Setenv("GOEPAY_PHP_REFERENCE_ROOT", "")
	if _, err := loadConfiguration(); err == nil || !strings.Contains(err.Error(), "explicit local runtime") {
		t.Fatal(err)
	}
}

func TestOriginalReferenceHashAndTraversalAreChecked(t *testing.T) {
	root := t.TempDir()
	name := "original.php"
	filename := filepath.Join(root, name)
	if err := os.WriteFile(filename, []byte("original bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	c := configuration{references: root, receipt: receipt{References: map[string]string{name: sum([]byte("original bytes"))}}}
	if err := verifyReferences(c); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("changed bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReferences(c); err == nil {
		t.Fatal("changed reference accepted")
	}
	c.receipt.References = map[string]string{"../outside.php": strings.Repeat("a", 64)}
	if err := verifyReferences(c); err == nil {
		t.Fatal("reference traversal accepted")
	}
}
