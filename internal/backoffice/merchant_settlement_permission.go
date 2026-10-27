package backoffice

func init() {
	d := definitions["merchants"]
	f := selectField("settle", "结算权限", "1", "0")
	f.Required = false
	f.Default = "1"
	f.AdminOnly = true
	f.Options = []Option{{Label: "开启", Value: "1"}, {Label: "关闭", Value: "0"}}
	d.fields = append(d.fields, f, Field{Name: "settlement_permission_revision", Label: "结算权限修订", Type: "string", ReadOnly: true})
	definitions["merchants"] = d
}
