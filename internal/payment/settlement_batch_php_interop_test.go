package payment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/testphp"
)

func TestOriginalSettlementBatches84FourCSVFormatsPHPInterop(t *testing.T) {
	testphp.Require(t)
	source := testphp.Path(t, "epay_release/admin/download.php")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "c4def4e03af461d890a4fd83e41a9b0ccbff230ee2e7c0e3fa551d9d3893aaab" {
		t.Fatal("original public export source changed")
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "download.php"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "runner.php"), []byte(settlementBatchPHP84), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out, err := testphp.Run(t, ctx, testphp.Spec{Mounts: []testphp.Mount{{Source: dir, Target: "/work"}}, Args: []string{"/work/runner.php"}})
	if err != nil {
		t.Fatal("isolated original CSV execution", err, string(out))
	}
	var original map[string]string
	if err = json.Unmarshal(out, &original); err != nil || len(original) != 4 {
		t.Fatal("all original four layouts required", err, string(out))
	}
	rows := []settlementBatchExportRow{{RequestID: 1, Position: 1, Type: "alipay", Account: "buyer84@example.test", Name: "测试甲", NetAmount: 4875}, {RequestID: 2, Position: 2, Type: "wxpay", Account: "openid84", Name: "测试乙", AppID: "wxoriginal84", NetAmount: 1950}, {RequestID: 3, Position: 3, Type: "bank", Account: "6222020000000084", Name: "测试丙", NetAmount: 100}}
	for _, format := range []string{"common", "alipay", "mybank", "wxpay"} {
		t.Run(format, func(t *testing.T) {
			want, e := base64.StdEncoding.DecodeString(original[format])
			if e != nil {
				t.Fatal(e)
			}
			file, e := renderSettlementBatchExport(SettlementBatch{Batch: "20261025000000000084", Count: 3}, rows, format, "平台结算")
			if e != nil || !bytes.Equal(file.Data, want) {
				t.Fatalf("original %s byte layout/encoding changed: Go %q; PHP %q; error %v", format, file.Data, want, e)
			}
		})
	}
	after, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(after) != digest {
		t.Fatal("original reference changed during test")
	}
}

// Only the original public formatter executes. The include, session, database
// and channel lookups are replaced with fixed synthetic, memory-only fixtures.
const settlementBatchPHP84 = `<?php
namespace lib { class Channel { static function get($id){return ['appwxmp'=>1];} static function getWeixin($id){return ['appid'=>'wxoriginal84'];} } }
namespace {
class FixtureResult84 { private $rows; function __construct($r){$this->rows=$r;} function fetch(){return array_shift($this->rows) ?? false;} }
class FixtureDB84 { function query($sql){
 $rows=[['id'=>1,'type'=>1,'account'=>'buyer84@example.test','username'=>'测试甲','realmoney'=>'48.75'],['id'=>2,'type'=>2,'account'=>'openid84','username'=>'测试乙','realmoney'=>'19.50'],['id'=>3,'type'=>4,'account'=>'6222020000000084','username'=>'测试丙','realmoney'=>'1.00']];
 if(str_contains($sql,'(type=1 or type=4)'))$rows=array_values(array_filter($rows,fn($r)=>in_array($r['type'],[1,4])));
 elseif(str_contains($sql,'and type=1'))$rows=array_values(array_filter($rows,fn($r)=>$r['type']===1));
 elseif(str_contains($sql,'and type=2'))$rows=array_values(array_filter($rows,fn($r)=>$r['type']===2));
 if(str_contains($sql,'order by type'))usort($rows,fn($a,$b)=>[$a['type'],$a['id']]<=>[$b['type'],$b['id']]);
 return new FixtureResult84($rows);
} }
 $source=file_get_contents('/work/download.php');
 $functions=strpos($source,'function display_type(');$switch=strpos($source,'switch($act)');$end=strpos($source,"case 'ustat':");
 if($functions===false||$switch===false||$end===false)throw new \Exception('original formatter boundary');
 eval(substr($source,$functions,$switch-$functions));
 $program=substr($source,$switch,$end-$switch)."\n}";
 $conf=['transfer_desc'=>'平台结算','transfer_wxpay'=>7];$DB=new FixtureDB84();$act='settle';$result=[];
 foreach(['common','alipay','mybank','wxpay'] as $format){$_GET=['type'=>$format,'batch'=>'20261025000000000084'];ob_start();eval($program);$result[$format]=base64_encode(ob_get_clean());}
 echo json_encode($result,JSON_THROW_ON_ERROR);
}
`
