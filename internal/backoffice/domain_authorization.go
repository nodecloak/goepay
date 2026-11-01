package backoffice

import "goepay/internal/merchantpolicy"

func authorizationDomainField() Field {
	f := text("domain", "域名", true, 253)
	f.Description = "支持 example.com 或最左侧通配符 *.example.com；通配符匹配一层及多层子域，不含根域名。仅填写域名，不含协议、路径或端口；大小写及单个末尾点会规范化。商户新增或修改域名后须由管理员审核。"
	return f
}

// A wildcard is a single literal leftmost label, never an arbitrary glob.
// The same normalization is used when storing and when reading approved rules.
func normalizeAuthorizationDomain(value string) (string, bool) {
	return merchantpolicy.NormalizeAuthorizationDomain(value)
}

func authorizationDomainMatches(host, rule string) bool {
	return merchantpolicy.AuthorizationDomainMatches(host, rule)
}
