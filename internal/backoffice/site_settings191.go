package backoffice

import (
	"encoding/json"
	"goepay/internal/siteinfo"
)

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			seen := map[string]bool{}
			for _, o := range d.fields[i].Options {
				seen[o.Value] = true
			}
			for _, key := range siteinfo.OriginalKeys {
				if !seen[key] {
					d.fields[i].Options = append(d.fields[i].Options, Option{Label: key, Value: key})
				}
			}
		}
	}
	definitions["settings"] = d
}

func siteSettingDefinition191(d definition, raw map[string]json.RawMessage, current map[string]any) definition {
	if !d.layoutSettings {
		return d
	}
	key, _ := current["key"].(string)
	if v, ok := raw["key"]; ok {
		if json.Unmarshal(v, &key) != nil {
			return d
		}
	}
	if !siteinfo.Optional(key) {
		return d
	}
	d.fields = append([]Field(nil), d.fields...)
	for i := range d.fields {
		if d.fields[i].Name == "value" {
			d.fields[i].Required = false
		}
	}
	return d
}
