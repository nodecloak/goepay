package backoffice

// Original admin/set.php 用户中心创建红包功能 (user_transfer_red).
func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "用户中心创建红包功能", Value: "user_transfer_red"})
		}
	}
	definitions["settings"] = d
}

func validateRedpackSetting(key, value string) (bool, error) {
	if key != "user_transfer_red" {
		return false, nil
	}
	if value != "0" && value != "1" {
		return true, ErrInvalid
	}
	return true, nil
}
