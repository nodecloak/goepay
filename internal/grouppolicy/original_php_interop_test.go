package grouppolicy

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

// Executes the actual local Release class. The fake SQL port contains only
// synthetic channel metadata; no original application bootstrap or credentials.
func TestOriginalGroupRoutingPHPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 requires the pinned offline PHP image and original class")
	}
	reference, err := filepath.Abs(filepath.Join("..", "..", "epay_release", "includes", "lib", "Channel.php"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "ae0ce2a8a0d6958c838fa70c889a0eae55cf806d21608eeec9ccb194c3bc3635"
	if got := fmt.Sprintf("%x", sha256.Sum256(content)); got != expected {
		t.Fatalf("original reference changed: %s", got)
	}
	dir := t.TempDir()
	harness := filepath.Join(dir, "runner.php")
	if err = os.WriteFile(harness, []byte(originalRoutingPHP153), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--pull", "never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--mount", "type=bind,source="+reference+",target=/reference/Channel.php,readonly", "--mount", "type=bind,source="+harness+",target=/runner.php,readonly", "sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50", "php", "/runner.php")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("offline original class: %v: %s", err, out)
	}
	var result struct{ Closed, First, Sequential, Random, NoEligible bool }
	if json.Unmarshal(out, &result) != nil || !result.Closed || !result.First || !result.Sequential || !result.Random || !result.NoEligible {
		t.Fatalf("original behavior mismatch: %s", out)
	}
	for sentinel, kind := range map[string]ChoiceKind{"0": Disabled, "-1": Random, "-4": Sequential, "-5": First} {
		info, e := ParseInfo([]byte(`{"alipay":{"channel":"` + sentinel + `","type":"channel","rate":"0"}}`))
		if e != nil || info["alipay"].RoutingChoice().Kind() != kind {
			t.Fatal("Go model differs from executed original", sentinel, e)
		}
	}
	t.Logf("original SHA256=%s; PHP closed/ascending first/shared sequence/random eligible/no eligible branches positive; routing only, not a channel fee equivalence claim", expected)
}

const originalRoutingPHP153 = `<?php
date_default_timezone_set('Asia/Shanghai');
require '/reference/Channel.php';
function isNullOrEmpty($value){ return $value === null || $value === ''; }
class LocalRoutingDB {
 public $channel='0'; public $index=0; public $empty=false;
 function getColumn($sql){
  if(str_contains($sql,'SELECT info FROM pre_group')) return json_encode([1=>['channel'=>$this->channel,'type'=>'channel','rate'=>'0'],2=>['channel'=>$this->channel,'type'=>'channel','rate'=>'0']]);
  if(str_contains($sql,'SELECT '.chr(96).'index'.chr(96).' FROM pre_group')) return $this->index;
  throw new Exception('Unexpected SQL read');
 }
 function getAll($sql){
  if(!preg_match("/FROM pre_channel WHERE type='([12])' AND status=1 AND daystatus=0 ORDER BY id ASC/",$sql,$m)) throw new Exception('Unexpected channel SQL');
  $ids=$m[1]==='1'?[8,7]:[10,9]; $rows=[];
  foreach($ids as $id) $rows[]=['id'=>$id,'plugin'=>'local','status'=>1,'rate'=>'98.75','apptype'=>'','mode'=>0,'paymin'=>$this->empty?'2.00':'','paymax'=>'','timestart'=>'','timestop'=>''];
  usort($rows,fn($a,$b)=>$a['id']<=>$b['id']); return $rows;
 }
 function exec($sql){
  if(!preg_match("/SET \x60index\x60='([0-9]+)' WHERE gid='42'/",$sql,$m)) throw new Exception('Unexpected SQL write');
  $this->index=(int)$m[1]; return 1;
 }
}
$DB=new LocalRoutingDB();
function choose($type=1){return \lib\Channel::getSubmitInfo($type,$type===1?'alipay':'wxpay',153,42,1.00);}
$result=['Closed'=>choose()===false];
$DB->channel='-5';$result['First']=choose()['channel']===7;
$DB->channel='-4';$chosen=[choose()['channel'],choose(2)['channel'],choose()['channel']];$result['Sequential']=$chosen===[7,10,7]&&$DB->index===1;
$DB->channel='-1';$result['Random']=true;for($i=0;$i<32;$i++){if(!in_array(choose()['channel'],[7,8],true))$result['Random']=false;}
$DB->empty=true;$DB->channel='-4';$before=$DB->index;$result['NoEligible']=choose()===false&&$DB->index===$before;
echo json_encode($result,JSON_THROW_ON_ERROR);
`
