package merchantnotice

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/testphp"
)

func TestOriginalProNotice80PHPTemplatesInterop(t *testing.T) {
	testphp.Require(t)
	type fixture struct {
		Scene  string         `json:"scene"`
		Site   string         `json:"site"`
		At     int64          `json:"at"`
		Params map[string]any `json:"params"`
	}
	// This is the pinned original Pro MsgNotice catalog. New Go-only internal
	// sources (such as payment_disabled) have their own real delivery tests.
	originalScenes := []string{"regaudit", "apply", "domain", "internal_transfer", "order", "settle", "login", "complain", "mchrisk", "balance", "group", "domain_audit", "channellimit", "daily_report"}
	cases := make([]fixture, 0, len(originalScenes))
	expected := map[string]Message{}
	for _, scene := range originalScenes {
		if _, ok := noticeSpecs[scene]; !ok {
			t.Fatal("original Pro notice template missing", scene)
		}
		in := input80(scene)
		in.Params["money"], in.Params["realmoney"], in.Params["msgmoney"] = "12.30", "12.00", "20.00"
		params := map[string]any{}
		for key, value := range in.Params {
			params[key] = value
		}
		if scene == "daily_report" {
			in.Daily = daily80()
			d := in.Daily
			params["date"], params["money"], params["getmoney"], params["order_count"], params["success_count"], params["success_rate"], params["has_transfer"], params["transfer_total"] = d.Date, "12.30", "12.00", 3, 2, "66.67", true, "5.00"
			params["type_money"] = []map[string]string{{"name": "微信", "money": "10.00"}, {"name": "支付宝", "money": "2.30"}}
			params["transfer_types"] = []map[string]string{{"name": "银行卡", "money": "5.00"}}
		}
		message, e := Render(in)
		if e != nil {
			t.Fatal(scene, e)
		}
		expected[scene] = message
		cases = append(cases, fixture{scene, in.SiteName, in.At.Unix(), params})
	}
	payload, e := json.Marshal(cases)
	if e != nil {
		t.Fatal(e)
	}
	out := runNoticePHP80(t, noticePHP80, payload)
	var got map[string]struct {
		Title string `json:"title"`
		HTML  string `json:"html"`
	}
	if e = json.Unmarshal(out, &got); e != nil || len(got) != len(expected) {
		t.Fatal("incomplete original template coverage", e)
	}
	for scene, want := range expected {
		t.Run(scene, func(t *testing.T) {
			actual, ok := got[scene]
			if !ok || actual.Title != want.Title || actual.HTML != want.HTML {
				t.Fatalf("original ordinary template changed\nGo: %s\nPHP: %s", want.HTML, actual.HTML)
			}
		})
	}

}

func runNoticePHP80(t *testing.T, script string, payload []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	source := testphp.Path(t, "epay_pro/app/lib/MsgNotice.php")
	raw, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	originalHash := sha256.Sum256(raw)
	dir := t.TempDir()
	if e = os.WriteFile(filepath.Join(dir, "MsgNotice.php"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "runner.php"), []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	out, e := testphp.Run(t, ctx, testphp.Spec{Mounts: []testphp.Mount{{Source: dir, Target: "/work"}}, Args: []string{"/work/runner.php"}, Input: payload})
	if e != nil {
		t.Fatalf("isolated public PHP renderer failed: %v %s", e, out)
	}
	after, e := os.ReadFile(source)
	if e != nil || sha256.Sum256(after) != originalHash {
		t.Fatal("original Pro template source changed")
	}
	return out
}

const noticePHP80 = `<?php
namespace app\lib {
 function config_get($name, $default = null) { return $name === 'sitename' ? $GLOBALS['fixture_site'] : $default; }
 function date($format, $timestamp = null) { return \date($format, $timestamp ?? $GLOBALS['fixture_now']); }
}
namespace {
 date_default_timezone_set('Asia/Shanghai');
 require '/work/MsgNotice.php';
 $cases=json_decode(stream_get_contents(STDIN),true,512,JSON_THROW_ON_ERROR);
 $method=new ReflectionMethod(\app\lib\MsgNotice::class,'get_msg_tpl');$method->setAccessible(true);
 $out=[];
 foreach($cases as $case) {
  $GLOBALS['fixture_site']=$case['site'];$GLOBALS['fixture_now']=$case['at'];
  [$title,$html]=$method->invoke(null,$case['scene'],$case['params']);
  $out[$case['scene']]=['title'=>$title,'html'=>$html];
 }
 echo json_encode($out,JSON_UNESCAPED_UNICODE|JSON_THROW_ON_ERROR);
}
`
