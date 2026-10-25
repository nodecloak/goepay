package backoffice

import "encoding/json"

type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type Field struct {
	CreateOnly bool     `json:"create_only,omitempty"`
	Name       string   `json:"name"`
	Label      string   `json:"label"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	ReadOnly   bool     `json:"read_only,omitempty"`
	WriteOnly  bool     `json:"write_only,omitempty"`
	MaxLength  int      `json:"max_length,omitempty"`
	Pattern    string   `json:"pattern,omitempty"`
	Min        *int64   `json:"min,omitempty"`
	Max        *int64   `json:"max,omitempty"`
	Options    []Option `json:"options,omitempty"`
	Default    any      `json:"default,omitempty"`
	AdminOnly  bool     `json:"-"`
}
type Permissions struct {
	Create bool `json:"create"`
	Update bool `json:"update"`
	Delete bool `json:"delete"`
}
type Schema struct {
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Fields      []Field     `json:"fields"`
	Permissions Permissions `json:"permissions"`
	Actions     []string    `json:"actions"`
}
type definition struct {
	title, description   string
	fields               []Field
	tenant, merchantRead bool
	noDelete             bool
	merchantReadOnly     bool
	superOnly            bool
}

func text(name, label string, required bool, max int) Field {
	return Field{Name: name, Label: label, Type: "string", Required: required, MaxLength: max}
}
func flag(name, label string, def bool) Field {
	return Field{Name: name, Label: label, Type: "boolean", Default: def}
}
func integer(name, label string, min, max int64) Field {
	return Field{Name: name, Label: label, Type: "integer", Min: &min, Max: &max}
}
func selectField(name, label string, values ...string) Field {
	f := text(name, label, true, 64)
	f.Type = "select"
	for _, v := range values {
		f.Options = append(f.Options, Option{Label: v, Value: v})
	}
	return f
}
func administrator(f Field) Field { f.AdminOnly = true; return f }
func createOnly(f Field) Field    { f.CreateOnly = true; return f }
func adminClassField() Field {
	f := selectField("admin_class", "权限等级", "operator", "super")
	f.Default = "operator"
	return f
}

var definitions = map[string]definition{
	"groups":              {title: "用户组设置", description: "保存用户组及费率配置；会员购买另走资金用例。", fields: []Field{text("name", "名称", true, 100), text("description", "说明", false, 1000), integer("rate_bps", "费率（万分比）", 0, 10000), integer("price_cents", "会员价格（分）", 0, 100000000000), integer("duration_days", "会员天数", 1, 3660), flag("enabled", "启用", true)}, merchantRead: true},
	"domains":             {title: "授权域名", tenant: true, fields: []Field{administrator(text("merchant_id", "商户 ID", false, 19)), text("domain", "域名", true, 253), administrator(flag("approved", "审核通过", false)), flag("enabled", "启用", true)}},
	"invitation-codes":    {title: "邀请码管理", fields: []Field{createOnly(text("code", "邀请码", true, 64)), integer("max_uses", "可使用次数", 1, 1000000), flag("enabled", "启用", true)}},
	"announcements":       {title: "公告配置", description: "正文使用纯文本，前端不应作为 HTML 执行。", fields: []Field{text("title", "标题", true, 200), {Name: "content", Label: "正文", Type: "text", Required: true, MaxLength: 20000}, selectField("audience", "接收对象", "all", "merchant", "admin"), flag("enabled", "发布", true)}},
	"settings":            {title: "系统设置", description: "只保存明确允许的业务设置。payment_fee_enabled=true 才按用户组费率扣费；refund_fee_payer=platform 退还手续费，merchant 保留手续费并由商户承担；这些支付费率只影响新订单快照。settlement_minimum_cents 为非负整数分，未配置时为0，适用于商户及管理员新建结算申请，历史请求不变。结算审批不代表外部出款。", fields: []Field{selectField("key", "设置项", "site_name", "support_email", "registration_open", "settlement_minimum_cents", "enforce_domains", "payment_fee_enabled", "refund_fee_payer"), text("value", "设置值", true, 1000)}},
	"channels":            {title: "支付通道", description: "通道资料管理。凭证通过受控配置加载；启用资料不会自动开放真实资金调用。", fields: []Field{text("name", "名称", true, 100), selectField("provider", "支付平台", "alipay", "wxpay"), selectField("method", "支付方式", "page", "wap", "native", "jsapi", "h5", "app"), integer("weight", "权重", 1, 1000), flag("enabled", "启用资料", false), text("remark", "备注", false, 1000)}},
	"blacklist":           {title: "黑名单管理", fields: []Field{selectField("kind", "类型", "ip", "domain", "buyer"), text("value", "匹配值", true, 253), text("reason", "原因", true, 1000), flag("enabled", "启用", true), {Name: "expires_at", Label: "到期时间（空为永久）", Type: "string", ReadOnly: true}}},
	"collection-styles":   {title: "聚合收款码样式", fields: collectionStyleFields()},
	"profitsharing-rules": {title: "站内分账规则", tenant: true, description: "规则只用于经过审批的站内账本分配；渠道分账不由本资源执行。", fields: []Field{administrator(text("merchant_id", "付款商户 ID", false, 19)), text("name", "名称", true, 100), text("recipient_id", "收款商户 ID", true, 19), integer("rate_bps", "分账比例（万分比）", 1, 10000), flag("enabled", "启用", true)}},
	"merchants":           {title: "商户管理", noDelete: true, fields: []Field{{Name: "id", Label: "商户 ID", Type: "string", Required: true, CreateOnly: true}, text("name", "名称", true, 100), {Name: "api_key", Label: "API 密钥", Type: "password", WriteOnly: true, MaxLength: 256}, selectField("signature_algorithm", "签名算法", "MD5", "RSA"), {Name: "rsa_public_key", Label: "商户 RSA 公钥", Type: "text", MaxLength: 8192, WriteOnly: true}, text("login_username", "后台登录账号（可选）", false, 100), {Name: "login_password", Label: "后台登录密码（首次设置或重置）", Type: "password", WriteOnly: true, MaxLength: 1024}, flag("enabled", "启用", true), {Name: "email", Label: "电子邮箱", Type: "email", MaxLength: 254}, text("group_id", "基础用户组 ID（管理员指定）", false, 19), {Name: "base_group_id", Label: "基础组账务快照", Type: "string", ReadOnly: true}, {Name: "group_baseline_unresolved", Label: "旧会员基础组待确认", Type: "boolean", ReadOnly: true}}},
	"admin-users":         {title: "管理员列表", noDelete: true, fields: []Field{createOnly(text("username", "账号", true, 100)), text("display_name", "名称", true, 100), {Name: "password", Label: "密码", Type: "password", WriteOnly: true, MaxLength: 1024}, adminClassField(), flag("enabled", "启用", true)}},
}

func (s *Service) Schema(p Principal, resource string) (Schema, error) {
	d, ok := definitions[resource]
	if !ok {
		return Schema{}, ErrNotFound
	}
	if !authorized(p, d, false) {
		return Schema{}, ErrForbidden
	}
	fields := append([]Field{}, d.fields...)
	for i := range fields {
		if p.Role != "admin" && fields[i].AdminOnly {
			fields[i].ReadOnly = true
		}
	}
	write := p.Role == "admin" || d.tenant && !d.merchantReadOnly
	if resource == "settlement-settings" || resource == "template-configs" {
		write = false
	}
	actions := []string{}
	switch resource {
	case "merchants":
		if p.Role == "admin" && p.AdminClass == "super" {
			actions = []string{"sso"}
		}
	case "complaints":
		actions = []string{"history", "comments"}
		if p.Role == "admin" {
			actions = append(actions, "review")
		}
	case "onboarding-applications":
		actions = []string{"history", "comments", "submit"}
		if p.Role == "admin" {
			actions = append(actions, "review")
		}
	case "profitsharing-rules":
		actions = []string{"execute"}
	}
	return Schema{Title: d.title, Description: d.description, Fields: fields, Permissions: Permissions{Create: write, Update: write, Delete: write && !d.noDelete}, Actions: actions}, nil
}
func decodeFields(raw []byte) map[string]any {
	v := map[string]any{}
	json.Unmarshal(raw, &v)
	return v
}
