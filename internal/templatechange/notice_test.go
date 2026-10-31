package templatechange

import (
	"strings"
	"testing"
	"time"
)

// 2026-10-31 10:20:05 UTC -> 18:20:05 UTC+8.
var at = time.Date(2026, 10, 31, 10, 20, 5, 0, time.UTC)

func TestMessageOperationWording(t *testing.T) {
	cases := []struct {
		n    Notice
		want string
	}{
		{
			Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "upload", Version: 4, Actor: "admin", At: at},
			"管理员 admin 于 2026-10-31 18:20:05（UTC+8）上传了首页模板「极光」（aurora）第 4 版。",
		},
		{
			Notice{Kind: "pay", Name: "neon", Title: "霓虹", Operation: "restore", Version: 5, RestoredFrom: 2, Actor: "root.1", At: at},
			"管理员 root.1 于 2026-10-31 18:20:05（UTC+8）把支付页模板「霓虹」（neon）恢复为第 2 版（新版本第 5 版）。",
		},
		{
			Notice{Kind: "qrpay", Name: "sakura-x", Title: "樱", Operation: "remove", Version: 3, Actor: "admin", At: at},
			"管理员 admin 于 2026-10-31 18:20:05（UTC+8）删除了聚合收款页模板「樱」（sakura-x）。",
		},
		{
			Notice{Kind: "home", Name: "aurora", Title: "极光", Operation: "select", Version: 7, Actor: "admin", At: at},
			"管理员 admin 于 2026-10-31 18:20:05（UTC+8）把当前首页模板切换为上传的「极光」（aurora）第 7 版。",
		},
	}
	for _, c := range cases {
		title, text, e := c.n.Message()
		if e != nil {
			t.Fatal(e, c.n.Operation)
		}
		if title != "页面模板已修改" {
			t.Fatal("wrong title", title)
		}
		if !strings.HasPrefix(text, c.want) {
			t.Fatal("wrong first line", c.n.Operation, text)
		}
		if !strings.HasSuffix(text, "如果不是你本人操作，请立即修改管理员密码，并在「模板配置」检查当前使用的模板。") {
			t.Fatal("missing warning line", c.n.Operation, text)
		}
	}
}

func TestMessageKindLabels(t *testing.T) {
	for kind, label := range map[string]string{"home": "首页模板", "pay": "支付页模板", "qrpay": "聚合收款页模板"} {
		n := Notice{Kind: kind, Name: "aurora", Title: "t", Operation: "upload", Version: 1, Actor: "admin", At: at}
		_, text, e := n.Message()
		if e != nil {
			t.Fatal(e, kind)
		}
		if !strings.Contains(text, label) {
			t.Fatal("wrong label", kind, text)
		}
	}
}

func TestMessageActorFallback(t *testing.T) {
	for _, actor := range []string{"", "bad name", "a\r\nb", "<script>", strings.Repeat("x", 65), "a/b"} {
		n := Notice{Kind: "home", Name: "aurora", Title: "t", Operation: "upload", Version: 1, Actor: actor, At: at}
		_, text, e := n.Message()
		if e != nil {
			t.Fatal(e, actor)
		}
		if !strings.Contains(text, "管理员 （未知） 于") {
			t.Fatal("unsafe actor not replaced", actor, text)
		}
	}
	for _, actor := range []string{"admin", "root.1", "a_b@c-d", "运营管理员", strings.Repeat("x", 64)} {
		n := Notice{Kind: "home", Name: "aurora", Title: "t", Operation: "upload", Version: 1, Actor: actor, At: at}
		_, text, e := n.Message()
		if e != nil {
			t.Fatal(e, actor)
		}
		if !strings.Contains(text, "管理员 "+actor+" 于") {
			t.Fatal("safe actor altered", actor, text)
		}
	}
}

func TestMessageTitleNormalisation(t *testing.T) {
	// Control characters dropped.
	_, text, e := Notice{Kind: "home", Name: "aurora", Title: "a\r\n\x00b\tc", Operation: "upload", Version: 1, Actor: "admin", At: at}.Message()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(text, "「ab\tc」") {
		t.Fatal("control chars not dropped", text)
	}

	// Empty (after control strip) becomes （无标题）.
	_, text, e = Notice{Kind: "home", Name: "aurora", Title: "\r\n\x00", Operation: "upload", Version: 1, Actor: "admin", At: at}.Message()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(text, "「（无标题）」") {
		t.Fatal("empty title not defaulted", text)
	}

	// Clipped to 40 runes (multibyte counts as one rune each).
	long := strings.Repeat("中", 50)
	_, text, e = Notice{Kind: "home", Name: "aurora", Title: long, Operation: "upload", Version: 1, Actor: "admin", At: at}.Message()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(text, "「"+strings.Repeat("中", 40)+"」") || strings.Contains(text, strings.Repeat("中", 41)) {
		t.Fatal("title not clipped to 40 runes", text)
	}
}

func TestMessageRejectsInvalid(t *testing.T) {
	cases := []Notice{
		{Kind: "bogus", Name: "aurora", Title: "t", Operation: "upload", Version: 1, Actor: "admin", At: at},                  // bad kind
		{Kind: "home", Name: "A", Title: "t", Operation: "upload", Version: 1, Actor: "admin", At: at},                        // bad name (uppercase/too short)
		{Kind: "home", Name: "a", Title: "t", Operation: "upload", Version: 1, Actor: "admin", At: at},                        // name too short (needs >=2)
		{Kind: "home", Name: "aurora", Title: "t", Operation: "bogus", Version: 1, Actor: "admin", At: at},                    // bad operation
		{Kind: "home", Name: "aurora", Title: "t", Operation: "upload", Version: 0, Actor: "admin", At: at},                   // version must be > 0
		{Kind: "home", Name: "aurora", Title: "t", Operation: "upload", Version: -1, Actor: "admin", At: at},                  // negative version
		{Kind: "home", Name: "aurora", Title: "t", Operation: "upload", Version: 1, Actor: "admin"},                           // zero time
		{Kind: "home", Name: "aurora", Title: "t", Operation: "restore", Version: 4, RestoredFrom: 0, Actor: "admin", At: at}, // restore needs restored_from>0
		{Kind: "home", Name: "aurora", Title: "t", Operation: "restore", Version: 4, RestoredFrom: 4, Actor: "admin", At: at}, // restored_from >= version
		{Kind: "home", Name: "aurora", Title: "t", Operation: "restore", Version: 4, RestoredFrom: 5, Actor: "admin", At: at}, // restored_from beyond version
		{Kind: "home", Name: "aurora", Title: "t", Operation: "upload", Version: 4, RestoredFrom: 1, Actor: "admin", At: at},  // non-restore must have restored_from==0
		{Kind: "home", Name: "aurora", Title: "t", Operation: "select", Version: 4, RestoredFrom: 1, Actor: "admin", At: at},  // non-restore must have restored_from==0
		{Kind: "home", Name: "aurora", Title: "t", Operation: "remove", Version: 4, RestoredFrom: 1, Actor: "admin", At: at},  // non-restore must have restored_from==0
	}
	for i, c := range cases {
		if _, _, e := c.Message(); e != ErrNotice {
			t.Fatal("invalid notice accepted", i, c)
		}
	}
}
