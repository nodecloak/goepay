package adminnotice

import (
	"fmt"
	"regexp"
	"time"
)

var errorCode = regexp.MustCompile(`^(?:[0-9]{5}:)?[A-Z][A-Z0-9_]*$`)

func channelStoppedMessage(channel, event int64, code string) (string, string, error) {
	if channel <= 0 || event <= 0 || len(code) > 128 || !errorCode.MatchString(code) {
		return "", "", ErrDelivery
	}
	return "支付通道自动关闭提醒", fmt.Sprintf("通道 %d 曾因自动付款异常停用，停用记录 %d。\n原异常代码：%s\n当前状态以管理后台为准。未知转账仍按原单号查询；恢复通道不会重发原转账。", channel, event, code), nil
}

var actorName = regexp.MustCompile(`^[\p{L}\p{N}_.@-]{1,64}$`)

// pageTemplateActor bounds the administrator name shown in a notice. An actor
// name with characters outside letters/digits/_.@- (or too long/empty) becomes
// 未知 rather than leaking an unexpected value into the message.
func pageTemplateActor(actor string) string {
	if actorName.MatchString(actor) {
		return actor
	}
	return "（未知）"
}

// pageTemplateChangedMessage builds the shared title and plain text for a
// 首页自定义 HTML version notice. version must be > 0; restoredFrom is 0 for a
// normal save, otherwise 0 < restoredFrom < version. The time is shown in
// UTC+8. The HTML itself is never included.
func pageTemplateChangedMessage(version, restoredFrom int64, actor string, at time.Time) (string, string, error) {
	if version <= 0 || restoredFrom < 0 || restoredFrom >= version || at.IsZero() {
		return "", "", ErrDelivery
	}
	name := pageTemplateActor(actor)
	when := at.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
	var line string
	if restoredFrom == 0 {
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）保存了首页自定义 HTML 第 %d 版。", name, when, version)
	} else {
		line = fmt.Sprintf("管理员 %s 于 %s（UTC+8）把首页自定义 HTML 恢复为第 %d 版（新版本第 %d 版）。", name, when, restoredFrom, version)
	}
	text := line + "\n如果不是你本人操作，请立即修改管理员密码，并在「模板配置 › 首页模板 › 自定义 HTML」恢复旧版本。"
	return "首页自定义 HTML 已修改", text, nil
}
