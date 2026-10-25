package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOriginalMiniLaunchEncodingPHPInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 required")
	}
	source, e := os.ReadFile(filepath.Join("..", "..", "..", "epay_pro", "app", "common.php"))
	if e != nil {
		t.Fatal(e)
	}
	helpers := "<?php\n"
	for _, name := range []string{"wxminipay_jump_path", "alipaymini_jump_scheme"} {
		start := strings.Index(string(source), "function "+name+"(")
		if start < 0 {
			t.Fatal("reference helper missing", name)
		}
		end := strings.Index(string(source)[start:], "\n}")
		if end < 0 {
			t.Fatal("reference helper boundary missing", name)
		}
		helpers += string(source)[start:start+end+2] + "\n"
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"helpers.php": helpers, "runner.php": miniEncodingPHPRunner} {
		if e = os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	wxComplete := "https://pay.example.invalid/oauth/mini/complete/wxpay/7/42/?state=" + strings.Repeat("a_", 32)
	aliComplete := strings.Replace(wxComplete, "/wxpay/7/", "/alipay/8/", 1)
	payload, _ := json.Marshal(map[string]string{"wx_complete": wxComplete, "ali_complete": aliComplete, "path": "pages/pay/index", "appid": "aliapp", "money": "1.01"})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "-i", "--mount", "type=bind,src="+dir+",dst=/work,readonly", "php:8.2-cli", "php", "/work/runner.php")
	cmd.Stdin = bytes.NewReader(payload)
	output, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("PHP encoding %v %s", e, output)
	}
	var result struct{ WxPage, AliScheme string }
	if e = json.Unmarshal(output, &result); e != nil {
		t.Fatalf("%v %s", e, output)
	}
	wx, e := miniLaunch("wxpay", &MiniProgramConfig{AppID: "wxapp", Path: "pages/pay/index"}, MiniLaunchRequest{TradeNo: "42", CompleteURL: wxComplete, Amount: 101})
	if e != nil {
		t.Fatal(e)
	}
	var material map[string]string
	if e = json.Unmarshal([]byte(wx.Material), &material); e != nil {
		t.Fatal(e)
	}
	ali, e := miniLaunch("alipay", &MiniProgramConfig{AppID: "aliapp", Path: "pages/pay/index"}, MiniLaunchRequest{TradeNo: "42", CompleteURL: aliComplete, Amount: 101})
	if e != nil {
		t.Fatal(e)
	}
	if material["path"] != result.WxPage || ali.Material != result.AliScheme {
		t.Fatal("original helper money/url encoding differs", material["path"], result.WxPage, ali.Material, result.AliScheme)
	}
}

// Invoke the unmodified original helpers. Their old internal callback routes
// are deliberately replaced by the new one-use completion context, retaining
// the original page fields and PHP encoding rules rather than the old trust model.
const miniEncodingPHPRunner = `<?php
require '/work/helpers.php';
$in=json_decode(stream_get_contents(STDIN),true);
function config_get($key){global $in;return in_array($key,['wxminipay_path','alipaymini_path'])?$in['path']:0;}
function request(){return (object)['siteurl'=>'https://pay.example.invalid/'];}
function bind_completion($page,$complete){[$path,$query]=explode('?',$page,2);parse_str($query,$params);if(array_keys($params)!==['money','url'])throw new Exception('unexpected reference material');$params['url']=$complete;return $path.'?'.http_build_query($params);}
$order=['trade_no'=>'42','realmoney'=>$in['money']];
$wx=bind_completion(wxminipay_jump_path($order),$in['wx_complete']);
parse_str(parse_url(alipaymini_jump_scheme($order,$in['appid']),PHP_URL_QUERY),$ali);
$page=bind_completion($ali['page'],$in['ali_complete']);
echo json_encode(['WxPage'=>$wx,'AliScheme'=>'alipays://platformapi/startapp?appId='.$ali['appId'].'&page='.urlencode($page)]);
`
