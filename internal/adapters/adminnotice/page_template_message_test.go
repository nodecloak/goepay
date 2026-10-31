package adminnotice

import (
	"strings"
	"testing"
	"time"
)

func TestPageTemplateChangedMessageSaveAndRestoreWording(t *testing.T) {
	// 2026-10-31 10:20:05 UTC -> 18:20:05 UTC+8.
	at := time.Date(2026, 10, 31, 10, 20, 5, 0, time.UTC)

	title, text, e := pageTemplateChangedMessage(4, 0, "admin", at)
	if e != nil {
		t.Fatal(e)
	}
	if title != "首页自定义 HTML 已修改" {
		t.Fatal("wrong title", title)
	}
	if !strings.Contains(text, "管理员 admin 于 2026-10-31 18:20:05（UTC+8）保存了首页自定义 HTML 第 4 版。") {
		t.Fatal("wrong save wording", text)
	}
	if !strings.Contains(text, "如果不是你本人操作，请立即修改管理员密码") || !strings.Contains(text, "恢复旧版本") {
		t.Fatal("missing warning", text)
	}
	if strings.Contains(text, "恢复为第") {
		t.Fatal("save message mentions restore", text)
	}

	_, text, e = pageTemplateChangedMessage(5, 2, "admin", at)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(text, "把首页自定义 HTML 恢复为第 2 版（新版本第 5 版）。") {
		t.Fatal("wrong restore wording", text)
	}
	if !strings.Contains(text, "如果不是你本人操作") {
		t.Fatal("restore missing warning", text)
	}
}

func TestPageTemplateChangedMessageActorFallback(t *testing.T) {
	at := time.Date(2026, 10, 31, 10, 20, 5, 0, time.UTC)
	for _, actor := range []string{"", "bad name", "a\r\nb", "<script>", strings.Repeat("x", 65), "a/b"} {
		_, text, e := pageTemplateChangedMessage(4, 0, actor, at)
		if e != nil {
			t.Fatal(e, actor)
		}
		if !strings.Contains(text, "管理员 （未知） 于") {
			t.Fatal("unsafe actor not replaced", actor, text)
		}
	}
	// Legitimate names (letters, digits, CJK, allowed punctuation) are kept.
	for _, actor := range []string{"admin", "root.1", "a_b@c-d", "运营管理员", strings.Repeat("x", 64)} {
		_, text, e := pageTemplateChangedMessage(4, 0, actor, at)
		if e != nil {
			t.Fatal(e, actor)
		}
		if !strings.Contains(text, "管理员 "+actor+" 于") {
			t.Fatal("safe actor altered", actor, text)
		}
	}
}

func TestPageTemplateChangedMessageRejectsInvalidInputs(t *testing.T) {
	at := time.Date(2026, 10, 31, 10, 20, 5, 0, time.UTC)
	cases := []struct {
		version, restoredFrom int64
		at                    time.Time
	}{
		{0, 0, at},          // version must be > 0
		{-1, 0, at},         // negative version
		{4, 4, at},          // restoredFrom must be < version
		{4, 5, at},          // restoredFrom beyond version
		{4, -1, at},         // negative restoredFrom
		{4, 0, time.Time{}}, // zero time
	}
	for _, c := range cases {
		if _, _, e := pageTemplateChangedMessage(c.version, c.restoredFrom, "admin", c.at); e != ErrDelivery {
			t.Fatal("invalid input accepted", c)
		}
	}
}
