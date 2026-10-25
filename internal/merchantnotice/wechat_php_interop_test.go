package merchantnotice

import (
	"encoding/json"
	"os"
	"testing"
)

func TestOriginalProNotice80PHPWechatMappingsInterop(t *testing.T) {
	if os.Getenv("GOEPAY_PHP_INTEROP") != "1" {
		t.Skip("GOEPAY_PHP_INTEROP=1 required")
	}
	type fixture struct {
		Scene  string            `json:"scene"`
		Params map[string]string `json:"params"`
		Config map[string]string `json:"config"`
	}
	cases := []fixture{}
	expected := map[string]WechatTemplate{}
	fields := map[string][]string{"order": {"no", "name", "money", "time", "outno"}, "settle": {"type", "account", "money", "realmoney", "time"}, "login": {"user", "time", "name", "ip", "iploc"}, "complain": {"order_no", "time", "reason", "type", "name"}, "balance": {"user", "time", "money", "msg"}, "wxtransfer": {"money", "time"}}
	for scene, keys := range fields {
		in := templateInput80(scene)
		if scene == "complain" {
			in.Params["reason"] = "原投诉原因80"
		}
		config := map[string]string{"login_wx": "7", "sitename": in.SiteName, "wxnotice_tpl_" + scene: "synthetic-template80"}
		mapping := map[string]string{}
		for _, key := range keys {
			original := "wxnotice_tpl_" + scene + "_" + key
			mapping[original] = key + "_field"
			config[original] = mapping[original]
		}
		got, err := BuildWechatTemplate(in, WechatTemplateConfig{ID: "synthetic-template80", ConsoleURL: "https://console.example.test", Fields: mapping})
		if err != nil {
			t.Fatal(scene, err)
		}
		expected[scene] = got
		cases = append(cases, fixture{scene, in.Params, config})
	}
	payload, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	out := runNoticePHP80(t, wechatPHP80, payload)
	var actual map[string]struct {
		To         string                         `json:"touser"`
		TemplateID string                         `json:"template_id"`
		Data       map[string]WechatTemplateValue `json:"data"`
	}
	if err = json.Unmarshal(out, &actual); err != nil || len(actual) != 6 {
		t.Fatal("incomplete original Wechat scene coverage", err, string(out))
	}
	for scene, want := range expected {
		t.Run(scene, func(t *testing.T) {
			got, exists := actual[scene]
			a, _ := json.Marshal(got.Data)
			b, _ := json.Marshal(want.Data)
			if !exists || got.To != "openid80" || got.TemplateID != want.TemplateID || string(a) != string(b) {
				t.Fatalf("original PHP mapping changed PHP=%s Go=%s", a, b)
			}
		})
	}
}

// Only infrastructure is replaced: configuration, request URL, unavailable
// mbstring codepoint helpers, and the final SDK call. The actual original
// MsgNotice::send_wechat_tplmsg method executes unchanged with network=none.
const wechatPHP80 = `<?php
namespace app\lib {
 function config_get($key,$default=null){return $GLOBALS['fixture_config'][$key]??$default;}
 function request(){return (object)['siteurl'=>'https://console.example.test/'];}
 function mb_strlen($s){if(\function_exists('mb_strlen'))return \mb_strlen($s);return \preg_match_all('/./us',$s);}
 function mb_substr($s,$start,$length){if(\function_exists('mb_substr'))return \mb_substr($s,$start,$length);return \implode('',\array_slice(\preg_split('//u',$s,-1,PREG_SPLIT_NO_EMPTY),$start,$length));}
}
namespace app\lib\wechat {
 class WechatAPI {
  public function __construct($id){if($id!=7)throw new \Exception('wrong fixture application');}
  public function sendTemplateMessage($to,$template,$url,$data){$GLOBALS['captured']=['touser'=>$to,'template_id'=>$template,'data'=>$data];return true;}
 }
}
namespace {
 require '/work/MsgNotice.php';
 $cases=json_decode(stream_get_contents(STDIN),true,512,JSON_THROW_ON_ERROR);$out=[];
 foreach($cases as $case){$GLOBALS['fixture_config']=$case['config'];unset($GLOBALS['captured']);\app\lib\MsgNotice::send_wechat_tplmsg($case['scene'],'openid80',$case['params']);$out[$case['scene']]=$GLOBALS['captured']??null;}
 echo json_encode($out,JSON_UNESCAPED_UNICODE|JSON_THROW_ON_ERROR);
}
`
