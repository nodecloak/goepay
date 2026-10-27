package server

import (
	"bytes"
	"context"
	"encoding/json"
	"goepay/internal/payment"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRisk177StandaloneOnceCLIIsDefaultOffAndReusesOriginalRun(t *testing.T) {
	f, _, _ := risk177Flow(t)
	ctx := context.Background()
	v := risk177Data(t, f.request("GET", "", f.csrf))
	risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
		s["auto_check_payip"] = "1"
		s["check_payip_second"] = "3600"
		s["check_payip_count"] = "1"
	}), f.csrf))
	if p := f.pay(t, "risk177-cli-first", "192.0.2.177"); p["code"] != float64(1) {
		t.Fatal(p)
	}
	goexe := "go"
	suffix := ""
	if runtime.GOOS == "windows" {
		goexe = "go.exe"
		suffix = ".exe"
	}
	binary := filepath.Join(t.TempDir(), "risk177-check"+suffix)
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", goexe), "build", "-o", binary, "./cmd/risk-check")
	build.Dir = "../.."
	if e := build.Run(); e != nil {
		t.Fatal("local CLI build failed", e)
	}
	run := func(enabled string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, "--once", "--external-no", "risk177-cli-original-run")
		env := []string{}
		for _, e := range os.Environ() {
			key := strings.ToUpper(strings.SplitN(e, "=", 2)[0])
			if key != "DATABASE_URL" && key != "GOEPAY_MASTER_KEY" && !strings.HasPrefix(key, "GOEPAY_ENABLE_") && !strings.HasPrefix(key, "GOEPAY_RISK_GEO_") {
				env = append(env, e)
			}
		}
		scoped, e := url.Parse(f.s.Pool.Config().ConnConfig.ConnString())
		if e != nil {
			return nil, e
		}
		q := scoped.Query()
		q.Set("search_path", f.s.Pool.Config().ConnConfig.RuntimeParams["search_path"])
		scoped.RawQuery = q.Encode()
		cmd.Env = append(env, "DATABASE_URL="+scoped.String(), "GOEPAY_MASTER_KEY="+strings.Repeat("0", 64), "GOEPAY_ENABLE_CORE_RISK_CHECK="+enabled)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &bytes.Buffer{}
		e = cmd.Run()
		return out.Bytes(), e
	}
	if _, e := run("false"); e == nil {
		t.Fatal("default-off CLI executed")
	}
	var before int
	if e := f.s.Pool.QueryRow(ctx, `SELECT count(*)FROM core_risk_check_runs`).Scan(&before); e != nil || before != 0 {
		t.Fatal("disabled CLI wrote a run", before, e)
	}
	var first payment.CoreRiskCheckResult
	for i := 0; i < 2; i++ {
		raw, e := run("true")
		if e != nil {
			t.Fatal("explicit local CLI execution failed", e)
		}
		var result payment.CoreRiskCheckResult
		if json.Unmarshal(raw, &result) != nil || result.ExternalNo != "risk177-cli-original-run" || result.Actions != 1 || result.Scanned != 1 {
			t.Fatal("CLI original result identity or counts mismatch")
		}
		if i == 0 {
			first = result
		} else if result != first {
			t.Fatal("CLI did not recover exact original result")
		}
	}
	var actions, runs int
	if e := f.s.Pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_risk_actions),(SELECT count(*)FROM core_risk_check_runs)`).Scan(&actions, &runs); e != nil || actions != 1 || runs != 1 {
		t.Fatal("CLI replay duplicated effects", actions, runs, e)
	}
}
