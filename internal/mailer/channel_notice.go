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

var channelNoticeCode = regexp.MustCompile(`^(?:[0-9]{5}:)?[A-Z][A-Z0-9_]*$`)

// A nil error means SMTP DATA acceptance, never inbox delivery. The caller
// must commit its one-shot notice claim before invoking this method.
func (s *SMTP) SendChannelStopped(ctx context.Context, to string, channel, event int64, code string) error {
	if s == nil || channel <= 0 || event <= 0 || len(code) > 128 || !channelNoticeCode.MatchString(code) || !mailbox(to) {
		return ErrSMTP
	}
	body := fmt.Sprintf("支付通道异常通知\r\n\r\n通道 %d 曾因自动付款异常停用，停用记录 %d。当前状态以管理后台为准。\r\n原异常代码：%s\r\n请在管理后台核对原转账及通道配置。未知结果仍按原单号查询；恢复通道不会重发原转账。\r\n", channel, event, code)
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	var lines strings.Builder
	for len(encoded) > 76 {
		lines.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	lines.WriteString(encoded + "\r\n")
	message := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", s.headerFrom(), to, mime.BEncoding.Encode("UTF-8", "支付通道异常停用通知"), time.Now().UTC().Format(time.RFC1123Z), lines.String()))
	return s.send(ctx, to, message)
}
