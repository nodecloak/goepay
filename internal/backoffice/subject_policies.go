package backoffice

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"goepay/internal/ordersubject"
)

func init() {
	template := text("template", "上游商品名称模板（空为继承）", false, 1024)
	template.Type = "text"
	template.Default = ""
	rules := text("rules", "金额规则 JSON（空继承，[]覆盖为空）", false, 20000)
	rules.Type = "text"
	rules.Default = ""
	definitions["order-subject-policies"] = definition{title: "上游商品名称策略", superOnly: true, description: "仅超级管理员。独立模板/金额规则按通道>商户>全局继承；通道ID是实际运行ID。金额规则为不重叠闭区间，min/max为元字符串。仅上游标题替换，原订单名与商户签名不变；首次发送快照固定，历史已发送订单不追溯。六变量[name]/[order]/[outorder]/[qq]/[phone]/[time]；|随机首次固定，显式渲染最多128UTF8字节。", fields: []Field{selectField("scope", "范围", "global", "merchant", "channel"), text("scope_id", "范围ID（global为0）", true, 19), template, rules, flag("enabled", "执行策略", false)}}
}
func validateSubjectPolicy(v map[string]any) error {
	scope, _ := v["scope"].(string)
	id, _ := v["scope_id"].(string)
	n, e := strconv.ParseInt(id, 10, 64)
	if e != nil || strconv.FormatInt(n, 10) != id {
		return ErrInvalid
	}
	template, _ := v["template"].(string)
	rules, _ := v["rules"].(string)
	if e = (ordersubject.Policy{Scope: scope, ScopeID: n, Template: template, RulesText: rules}).Validate(); e != nil {
		return ErrInvalid
	}
	return nil
}
func validateSubjectPolicyRelations(ctx context.Context, tx pgx.Tx, v map[string]any) error {
	if v["scope"] != "merchant" {
		return nil
	}
	var exists bool
	e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)", v["scope_id"]).Scan(&exists)
	if e != nil {
		return e
	}
	if !exists {
		return ErrInvalid
	}
	return nil
}
