package mailer

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"regexp"
	"strings"
	"time"
)

var pageTemplateActorName = regexp.MustCompile(`^[\p{L}\p{N}_.@-]{1,64}$`)

func pageTemplateNoticeActor(actor string) string {
	if pageTemplateActorName.MatchString(actor) {
		return actor
	}
	return "（未知）"
}

// SendPageTemplateChanged submits a bounded, non-PII administrator notice that
// the 首页自定义 HTML template gained a new version. A nil error means SMTP DATA
// acceptance only, never inbox delivery; the caller must commit its one-shot
// claim before invoking this. version > 0; restoredFrom is 0 for a normal save
// else 0 < restoredFrom < version; the time is shown in UTC+8. The HTML source
// text is never included.
func (s *SMTP) SendPageTemplateChanged(ctx context.Context, to string, version, restoredFrom int64, actor string, at time.Time) error {
	if s == nil || version <= 0 || restoredFrom < 0 || restoredFrom >= version || at.IsZero() || !mailbox(to) {
		return ErrSMTP
	}
	name := pageTemplateNoticeActor(actor)
	when := at.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
	var line string
	if restoredFrom == 0 {
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）保存了首页自定义 HTML 第 %d 版。", name, when, version)
	} else {
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）把首页自定义 HTML 恢复为第 %d 版（新版本第 %d 版）。", name, when, restoredFrom, version)
	}
	body := line + "\r\n如果不是你本人操作，请立即修改管理员密码，并在「模板配置 › 首页模板 › 自定义 HTML」恢复旧版本。\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var lines strings.Builder
	for len(encoded) > 76 {
		lines.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	lines.WriteString(encoded + "\r\n")
	message := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", s.headerFrom(), to, mime.BEncoding.Encode("UTF-8", "首页自定义 HTML 已修改"), time.Now().UTC().Format(time.RFC1123Z), lines.String()))
	return s.send(ctx, to, message)
}
