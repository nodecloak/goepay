package backoffice

func init() {
	d := definitions["settings"]
	for i := range d.fields {
		if d.fields[i].Name == "key" {
			d.fields[i].Options = append(d.fields[i].Options, Option{Label: "测试支付收款商户ID", Value: "test_pay_uid"})
		}
	}
	d.description += " test_pay_uid为原测试支付的固定收款商户ID，0关闭测试支付；界面请求不能改选商户或回调。测试仍经过普通支付编排和当前通道权限。"
	definitions["settings"] = d
}
