package backoffice

// A read-only projection of the existing merchant and ledger records. Income
// held for an individual order is separate from the legacy reserved balance.
// No account number, API key or encrypted payment material is selected.
const merchantConsoleFrom = `merchants LEFT JOIN LATERAL (
 SELECT COALESCE(sum(balance) FILTER(WHERE kind='available'),0)::text AS available_minor,
 COALESCE(sum(balance) FILTER(WHERE kind IN ('reserved','settlement_reserve','channel_sharing_reserve')),0)::text AS frozen_minor,
 COALESCE(sum(balance) FILTER(WHERE kind='order_income_reserve'),0)::text AS income_held_minor
 FROM accounts WHERE owner_id=merchants.id
) console_balances ON true
 LEFT JOIN memberships console_membership ON console_membership.merchant_id=merchants.id
 LEFT JOIN LATERAL (SELECT
 CASE WHEN console_membership.expires_at>statement_timestamp() THEN console_membership.group_id
 WHEN group_baseline_unresolved THEN NULL::bigint
 ELSE merchants.base_group_id END AS group_id,
 COALESCE(console_membership.expires_at>statement_timestamp(),false) AS active
 ) console_effective_group ON true
 LEFT JOIN backoffice_records console_group ON console_group.id=console_effective_group.group_id AND console_group.resource='groups'`

const merchantConsoleProjection = `jsonb_strip_nulls(jsonb_build_object(
 'email',CASE WHEN jsonb_typeof(profile->'email')='string' THEN profile->'email' END,
 'group_id',CASE WHEN jsonb_typeof(profile->'group_id')='string' THEN profile->'group_id' END,
 'transfer_api_enabled',CASE WHEN jsonb_typeof(profile->'transfer_api_enabled')='boolean' THEN profile->'transfer_api_enabled' END,
 'transfer_rate',CASE WHEN jsonb_typeof(profile->'transfer_rate')='string' THEN profile->'transfer_rate' END))||jsonb_build_object(
 'name',name,'enabled',enabled,'pay',COALESCE(to_jsonb(merchants)->>'pay','1'),'signature_algorithm',signature_algorithm,'id',merchants.id::text,
 'base_group_id',base_group_id::text,'group_baseline_unresolved',group_baseline_unresolved,
 'login_username',COALESCE((SELECT username FROM console_users WHERE role='merchant' AND subject_id=merchants.id ORDER BY id LIMIT 1),''),
 'available_minor',console_balances.available_minor,'frozen_minor',console_balances.frozen_minor,
 'income_held_minor',console_balances.income_held_minor,'created_at',merchants.created_at,
 'effective_group_id',COALESCE(console_effective_group.group_id::text,''),
 'effective_group_name',CASE WHEN console_effective_group.group_id IS NULL THEN '' ELSE COALESCE(console_group.body->>'name',CASE WHEN console_effective_group.group_id=0 THEN '默认用户组' ELSE '' END) END,
 'membership_expires_at',COALESCE(to_jsonb(console_membership.expires_at),'""'::jsonb),
 'group_membership_active',console_effective_group.active,
 'approved_domains',COALESCE((SELECT jsonb_agg(DISTINCT lower(domain_record.body->>'domain') ORDER BY lower(domain_record.body->>'domain'))
 FROM backoffice_records domain_record WHERE domain_record.resource='domains' AND domain_record.owner_id=merchants.id
 AND domain_record.body->>'approved'='true' AND domain_record.body->>'enabled'='true'),'[]'::jsonb))`

func init() {
	d := definitions["merchants"]
	for _, f := range []Field{
		{Name: "available_minor", Label: "账户余额（分）", Type: "string", ReadOnly: true},
		{Name: "frozen_minor", Label: "冻结预留（分）", Type: "string", ReadOnly: true},
		{Name: "income_held_minor", Label: "待解冻收入（分）", Type: "string", ReadOnly: true},
		{Name: "created_at", Label: "注册时间", Type: "string", ReadOnly: true},
		{Name: "effective_group_id", Label: "当前有效用户组 ID", Type: "string", ReadOnly: true},
		{Name: "effective_group_name", Label: "当前有效用户组", Type: "string", ReadOnly: true},
		{Name: "membership_expires_at", Label: "会员到期时间", Type: "string", ReadOnly: true},
		{Name: "group_membership_active", Label: "当前会员有效", Type: "boolean", ReadOnly: true},
		{Name: "approved_domains", Label: "已审核启用域名", Type: "json", ReadOnly: true},
		{Name: "settlement_recipient", Label: "结算账号与姓名（已脱敏）", Type: "json", ReadOnly: true},
	} {
		d.fields = append(d.fields, f)
	}
	definitions["merchants"] = d
}
