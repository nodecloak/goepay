package backoffice

import (
	"fmt"
	"strconv"
	"strings"
)

func originalAnnouncementFields() []Field {
	sort := integer("sort", "排序", 1, 2147483647)
	sort.Default = "50"
	color := text("color", "文字颜色", false, 7)
	color.Pattern, color.Default = `^$|^#[0-9a-fA-F]{6}$`, ""
	format := selectField("content_format", "内容格式", "text", "html")
	format.Required, format.Default = false, "text"
	audience := selectField("audience", "接收对象", "all", "merchant", "admin")
	audience.Default = "merchant"
	return []Field{
		text("title", "兼容旧标题", false, 200),
		{Name: "content", Label: "公告内容", Type: "text", Required: true, MaxLength: 20000},
		sort, color, format, audience, flag("enabled", "显示", true),
	}
}

func validateAnnouncement(v map[string]any) error {
	content, ok := v["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return ErrInvalid
	}
	format := "text"
	if value, exists := v["content_format"]; exists {
		var valid bool
		format, valid = value.(string)
		if !valid || format != "text" && format != "html" {
			return ErrInvalid
		}
	}
	if format == "html" && (strings.TrimSpace(content) == "<p><br></p>" || strings.TrimSpace(content) == "<p></p>") {
		return ErrInvalid
	}
	if value, exists := v["color"]; exists {
		color, valid := value.(string)
		if !valid || color != "" && !colorPattern.MatchString(color) {
			return ErrInvalid
		}
	}
	if value, exists := v["sort"]; exists {
		n, e := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		if e != nil || n < 1 || n > 2147483647 {
			return ErrInvalid
		}
	}
	return nil
}
