package payment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOriginalGroupSettlementConfigPHPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 requires pinned offline PHP and local original group config helper")
	}
	ref, e := filepath.Abs(filepath.Join("..", "..", "epay_release", "includes", "functions.php"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(ref)
	if e != nil {
		t.Fatal(e)
	}
	const expected = "5c90a36916e984c7f558b76c7f470e550b58cbe073fa8bd8382e130006202865"
	if hash := fmt.Sprintf("%x", sha256.Sum256(b)); hash != expected {
		t.Fatal("original config source changed", hash)
	}
	dir := t.TempDir()
	runner := filepath.Join(dir, "runner.php")
	if e = os.WriteFile(runner, []byte(groupSettlementPHP155), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--pull", "never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--mount", "type=bind,source="+ref+",target=/reference/functions.php,readonly", "--mount", "type=bind,source="+runner+",target=/runner.php,readonly", "sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50", "php", "/runner.php")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("offline original config helper: %v: %s", e, out)
	}
	var original []json.RawMessage
	if json.Unmarshal(out, &original) != nil || len(original) != 5 {
		t.Fatalf("unexpected original result: %s", out)
	}
	got := make([]map[string]json.RawMessage, len(original))
	for i, raw := range original {
		if string(raw) == "[]" {
			continue
		}
		if json.Unmarshal(raw, &got[i]) != nil {
			t.Fatalf("unexpected original config shape: %s", raw)
		}
	}
	for i, want := range []string{"0", "2.55", "", "", "0"} {
		rate, e := thresholdGroupScalar(got[i]["settle_rate"])
		if e != nil || rate != want {
			t.Fatal("Go scalar does not retain original empty/zero distinction", i, rate, want, e)
		}
	}
	if _, exists := got[0]["settle_open"]; exists {
		t.Fatal("original input-key zero did not inherit")
	}
	if _, exists := got[0]["user_transfer"]; exists {
		t.Fatal("original ordinary transfer zero did not inherit")
	}
	t.Logf("original functions SHA256=%s; getGroupConfig actual zero/string/numeric/empty/null branches positive; no application bootstrap or funds", expected)
}

const groupSettlementPHP155 = `<?php
require '/reference/functions.php';
class LocalGroupConfigDB {
 public $config=[];
 function getRow($sql){
  if(!preg_match("/SELECT config FROM pre_group WHERE gid='([0-9]+)' LIMIT 1/",$sql,$m))throw new Exception('Unexpected SQL');
  $id=(int)$m[1];return array_key_exists($id,$this->config)?['config'=>json_encode($this->config[$id],JSON_THROW_ON_ERROR)]:false;
 }
}
$DB=new LocalGroupConfigDB();$DB->config=[0=>['settle_rate'=>'0','settle_open'=>'0','user_transfer'=>'0'],7=>['settle_rate'=>'2.55'],8=>['settle_rate'=>''],9=>['settle_rate'=>null],10=>['settle_rate'=>0]];
$out=[];foreach([0,7,8,9,10] as $gid){$out[]=getGroupConfig($gid);}
echo json_encode($out,JSON_THROW_ON_ERROR);
`
