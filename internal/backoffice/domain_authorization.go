package backoffice

import (
	"net"
	"strings"
)

func authorizationDomainField() Field {
	f := text("domain", "域名", true, 253)
	f.Description = "支持 example.com 或最左侧通配符 *.example.com；通配符匹配一层及多层子域，不含根域名。仅填写域名，不含协议、路径或端口；大小写及单个末尾点会规范化。商户新增或修改域名后须由管理员审核。"
	return f
}

// A wildcard is a single literal leftmost label, never an arbitrary glob.
// The same normalization is used when storing and when reading approved rules.
func normalizeAuthorizationDomain(value string) (string, bool) {
	value = strings.TrimSuffix(strings.ToLower(value), ".")
	base := value
	if strings.HasPrefix(base, "*.") {
		base = strings.TrimPrefix(base, "*.")
	}
	if len(value) > 253 || !domainPattern.MatchString(base) || net.ParseIP(base) != nil {
		return "", false
	}
	return value, true
}

func authorizationDomainMatches(host, rule string) bool {
	// Host is the parsed URL hostname. Reject wildcard/URL/path/port text here
	// rather than interpreting it as another pattern supplied by a caller.
	if strings.Contains(host, "*") {
		return false
	}
	host, validHost := normalizeAuthorizationDomain(host)
	rule, validRule := normalizeAuthorizationDomain(rule)
	if !validHost || !validRule {
		return false
	}
	if strings.HasPrefix(rule, "*.") {
		base := strings.TrimPrefix(rule, "*.")
		return host != base && strings.HasSuffix(host, "."+base)
	}
	// Preserve existing plain-domain authorization of both root and subdomains.
	return host == rule || strings.HasSuffix(host, "."+rule)
}
