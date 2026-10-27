package payment

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

type historical73Facts struct {
	Fields             map[string]json.RawMessage `json:"fields"`
	Schema             string                     `json:"schema"`
	PlanID             int64                      `json:"plan_id"`
	Revision           int64                      `json:"revision"`
	AuthorizationID    int64                      `json:"authorization_id"`
	RunID              int64                      `json:"run_id"`
	RequestID          int64                      `json:"request_id"`
	ProofHash          []byte                     `json:"proof_hash"`
	TableHashes        map[string]string          `json:"table_hashes"`
	DumpSHA256         string                     `json:"dump_sha256"`
	UncompressedSHA256 string                     `json:"dump_uncompressed_sha256"`
	UncompressedBytes  int64                      `json:"dump_uncompressed_bytes"`
}

// The fixture contains synthetic financial facts written and authenticated by
// verified historical148 code against schema73. Current writers are deliberately
// never called against a partially migrated database to manufacture history.
func actualHistorical73Store(t *testing.T) (*Store, historical73Facts) {
	return actualHistoricalStore155(t, "settlement_schema73")
}

func (f historical73Facts) decode(t *testing.T, key string, to any) {
	t.Helper()
	if e := json.Unmarshal(f.Fields[key], to); e != nil {
		t.Fatal("historical fixture field", key, e)
	}
}

func actualHistoricalStore155(t *testing.T, name string) (*Store, historical73Facts) {
	t.Helper()
	var facts historical73Facts
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(name) {
		t.Fatal("invalid fixture name")
	}
	meta, e := os.ReadFile("testdata/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(meta, &facts); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("testdata/" + name + ".sql.gz")
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != facts.DumpSHA256 {
		t.Fatal("historical fixture checksum changed")
	}
	z, e := gzip.NewReader(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	raw, e = io.ReadAll(io.LimitReader(z, 8<<20))
	if e != nil {
		t.Fatal(e)
	}
	digest = sha256.Sum256(raw)
	if int64(len(raw)) != facts.UncompressedBytes || hex.EncodeToString(digest[:]) != facts.UncompressedSHA256 {
		t.Fatal("historical SQL checksum changed")
	}
	if !regexp.MustCompile(`^member72_[a-f0-9]{16}$`).MatchString(facts.Schema) {
		t.Fatal("invalid original schema")
	}
	s := membership72HistoricalStore(t, 0)
	ctx := context.Background()
	var schema string
	if e = s.pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); e != nil {
		t.Fatal(e)
	}
	if !regexp.MustCompile(`^member72_[a-f0-9]{16}$`).MatchString(schema) {
		t.Fatal("invalid restored schema")
	}
	lines := strings.Split(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, `\restrict `) || strings.HasPrefix(line, `\unrestrict `) {
			continue
		}
		if strings.HasPrefix(line, `\`) {
			t.Fatal("unsupported dump metacommand")
		}
		if line == "CREATE SCHEMA "+facts.Schema+";" {
			continue
		}
		// Renaming only object qualifiers must never change an original row value,
		// authorization scope, proof, journal, or request.
		if strings.HasPrefix(line, "INSERT INTO ") {
			if p := strings.Index(line, " VALUES "); p >= 0 && strings.Contains(line[p:], facts.Schema) {
				t.Fatal("schema rename would mutate original row data")
			}
		}
		kept = append(kept, strings.ReplaceAll(line, facts.Schema, schema))
	}
	sql := strings.Join(kept, "\n") + "\nSET search_path TO " + schema + ";"
	if _, e = s.pool.Exec(ctx, sql); e != nil {
		t.Fatal("restore genuine schema73 backup", e)
	}
	return s, facts
}
