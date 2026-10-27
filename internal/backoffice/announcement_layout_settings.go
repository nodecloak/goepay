package backoffice

import "encoding/json"

func originalLayoutSetting(key string) bool {
	return key == "modal" || key == "zhuce" || key == "footer"
}

func init() {
	d := definitions["settings"]
	d.layoutSettings = true
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "用户中心弹出公告", Value: "modal"}, Option{Label: "用户注册页面公告", Value: "zhuce"}, Option{Label: "底部版权", Value: "footer"})
		}
	}
	d.description += " modal/zhuce/footer为原公告排版HTML，最多20000字节；留空关闭对应展示。仅这些排版设置允许空值，其他设置原约束不变。"
	definitions["settings"] = d
}

func layoutSettingDefinition(d definition, raw map[string]json.RawMessage, current map[string]any) definition {
	if !d.layoutSettings {
		return d
	}
	key, _ := current["key"].(string)
	if value, ok := raw["key"]; ok {
		if json.Unmarshal(value, &key) != nil {
			return d
		}
	}
	if !originalLayoutSetting(key) {
		return d
	}
	d.fields = append([]Field(nil), d.fields...)
	for i := range d.fields {
		if d.fields[i].Name == "value" {
			d.fields[i].MaxLength = 20000
			d.fields[i].Required = false
		}
	}
	return d
}
