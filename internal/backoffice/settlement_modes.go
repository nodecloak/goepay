package backoffice

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "结算开关", Value: "settle_open"})
		}
	}
	definitions["settings"] = d
}
