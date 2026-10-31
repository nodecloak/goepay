// Package templatechange builds the shared title and plain text for an
// administrator notice that an uploaded page template changed. It is a leaf
// package with no transport or storage dependencies: the transports and the
// mailer render Notice.Message() into their own formats, and the delivery
// worker populates a Notice from the stored event.
package templatechange

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrNotice marks a Notice whose fields do not describe a well-formed template
// change. A sender must treat it as invalid input and never contact a network.
var ErrNotice = errors.New("administrator template notice input invalid")

// Notice describes one administrator template change. Kind is the template
// family (home|pay|qrpay); Name is the template name; Title is its display
// title; Operation is upload|restore|remove|select. Version is the resulting
// version (> 0). RestoredFrom is the source version for a restore
// (0 < RestoredFrom < Version) and 0 for every other operation. Actor is the
// administrator name and At is the event time.
type Notice struct {
	Kind         string
	Name         string
	Title        string
	Operation    string
	Version      int64
	RestoredFrom int64
	Actor        string
	At           time.Time
}

var noticeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)

// actorName bounds the administrator name shown in a notice. A name with
// characters outside letters/digits/_.@- (or too long/empty) becomes （未知）
// rather than leaking an unexpected value into the message.
var actorName = regexp.MustCompile(`^[\p{L}\p{N}_.@-]{1,64}$`)

func noticeActor(actor string) string {
	if actorName.MatchString(actor) {
		return actor
	}
	return "（未知）"
}

var kindLabel = map[string]string{
	"home":  "首页模板",
	"pay":   "支付页模板",
	"qrpay": "聚合收款页模板",
}

// noticeTitle drops control characters, clips to 40 runes, and renders an empty
// title as （无标题）.
func noticeTitle(title string) string {
	runes := make([]rune, 0, len(title))
	for _, r := range title {
		if r == '\t' || r >= 0x20 && r != 0x7f {
			runes = append(runes, r)
		}
	}
	if len(runes) > 40 {
		runes = runes[:40]
	}
	if len(runes) == 0 {
		return "（无标题）"
	}
	return string(runes)
}

// Message renders the shared title and plain two-line text. The title is
// always 页面模板已修改. The time is shown in UTC+8. An invalid Notice returns
// ErrNotice without producing any text.
func (n Notice) Message() (title, text string, err error) {
	label, ok := kindLabel[n.Kind]
	if !ok || !noticeName.MatchString(n.Name) || n.Version <= 0 || n.At.IsZero() {
		return "", "", ErrNotice
	}
	switch n.Operation {
	case "restore":
		if n.RestoredFrom <= 0 || n.RestoredFrom >= n.Version {
			return "", "", ErrNotice
		}
	case "upload", "remove", "select":
		if n.RestoredFrom != 0 {
			return "", "", ErrNotice
		}
	default:
		return "", "", ErrNotice
	}

	actor := noticeActor(n.Actor)
	clipped := noticeTitle(n.Title)
	when := n.At.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
	var line string
	switch n.Operation {
	case "upload":
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）上传了%s「%s」（%s）第 %d 版。", actor, when, label, clipped, n.Name, n.Version)
	case "restore":
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）把%s「%s」（%s）恢复为第 %d 版（新版本第 %d 版）。", actor, when, label, clipped, n.Name, n.RestoredFrom, n.Version)
	case "remove":
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）删除了%s「%s」（%s）。", actor, when, label, clipped, n.Name)
	case "select":
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）把当前%s切换为上传的「%s」（%s）第 %d 版。", actor, when, label, clipped, n.Name, n.Version)
	}
	text = line + "\n如果不是你本人操作，请立即修改管理员密码，并在「模板配置」检查当前使用的模板。"
	return "页面模板已修改", text, nil
}
