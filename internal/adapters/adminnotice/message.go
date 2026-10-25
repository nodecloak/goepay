package adminnotice

import (
	"fmt"
	"regexp"
)

var errorCode = regexp.MustCompile(`^(?:[0-9]{5}:)?[A-Z][A-Z0-9_]*$`)

func channelStoppedMessage(channel, event int64, code string) (string, string, error) {
	if channel <= 0 || event <= 0 || len(code) > 128 || !errorCode.MatchString(code) {
		return "", "", ErrDelivery
	}
	return "支付通道自动关闭提醒", fmt.Sprintf("通道 %d 曾因自动付款异常停用，停用记录 %d。\n原异常代码：%s\n当前状态以管理后台为准。未知转账仍按原单号查询；恢复通道不会重发原转账。", channel, event, code), nil
}
