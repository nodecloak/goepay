package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestKeygenPrintsOnlyPublicAddressesAndNeverOverwrites(t *testing.T) {
	dir := keygenTestDir(t)
	var output bytes.Buffer
	if err := run([]string{"-dir", dir, "-key-id", "deposit-wallet"}, &output); err != nil {
		t.Fatal(err)
	}
	var public map[string]string
	if err := json.Unmarshal(output.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if len(public) != 3 || public["key_id"] != "deposit-wallet" || public["tron_address"] == "" || public["ethereum_address"] == "" {
		t.Fatalf("unexpected public output: %s", output.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "deposit-wallet.key"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), bytes.TrimSpace(raw)) {
		t.Fatal("private key printed")
	}
	output.Reset()
	if err = run([]string{"-dir", dir, "-key-id", "deposit-wallet"}, &output); err == nil || output.Len() != 0 {
		t.Fatalf("overwrite returned public success: %q %v", output.String(), err)
	}
}

func TestKeygenRequiresExplicitPrivateDirectoryAndKeyID(t *testing.T) {
	for _, args := range [][]string{nil, {"-dir", "."}, {"-key-id", "wallet"}} {
		var output bytes.Buffer
		if err := run(args, &output); err == nil || output.Len() != 0 {
			t.Fatalf("incomplete command accepted: %v", args)
		}
	}
}
