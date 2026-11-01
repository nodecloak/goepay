package backoffice

import (
	"context"
	"time"
)

func administratorResourceFields() []Field {
	return []Field{
		createOnly(text("username", "账号", true, 100)), text("display_name", "名称", true, 100),
		{Name: "password", Label: "密码", Type: "password", WriteOnly: true, MaxLength: 1024},
		adminClassField(),
		{Name: "permissions", Label: "菜单权限", Type: "permission_tree", Description: "仅当权限等级为 operator 时生效：留空表示全部业务菜单，勾选则仅限所选菜单。super 忽略此项。"},
		flag("enabled", "启用", true),
		{Name: "created_at", Label: "添加时间", Type: "string", ReadOnly: true, Description: "来自管理员创建审计；未记录的历史账号不推算时间。"},
		{Name: "last_login_at", Label: "上次登录", Type: "string", ReadOnly: true, Description: "最近保留的成功管理员登录记录；失败及商户身份登录不计入。"},
		{Name: "totp_enabled", Label: "TOTP状态", Type: "boolean", ReadOnly: true, Description: "已确认启用的身份验证器；待确认绑定不计为启用。"},
	}
}

// One bounded summary query enriches the already-authorized page or detail.
// Missing audit history remains null. Neither hashes nor MFA secrets leave SQL.
func (s *Service) attachAdministratorMetadata(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	byID := make(map[int64]map[string]any, len(items))
	for i, item := range items {
		ids[i] = item.ID
		byID[item.ID] = item.Fields
	}
	rows, err := s.pool.Query(ctx, `
 WITH chosen AS (SELECT id,username FROM console_users WHERE role='admin' AND id=ANY($1::bigint[])),
 creations AS (
  SELECT resource_id,min(created_at) AS created_at FROM backoffice_audit
  WHERE resource='admin-users' AND operation='create' AND resource_id=ANY($1::bigint[]) GROUP BY resource_id
 ), logins AS (
  SELECT username,max(created_at) AS last_login_at FROM console_login_audit
  WHERE role='admin' AND success AND username IN (SELECT username FROM chosen) GROUP BY username
 )
 SELECT u.id,c.created_at,l.last_login_at,m.secret IS NOT NULL
 FROM chosen u LEFT JOIN creations c ON c.resource_id=u.id
 LEFT JOIN logins l ON l.username=u.username
 LEFT JOIN console_mfa m ON m.username=u.username AND m.role='admin'`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var created, lastLogin *time.Time
		var enabled bool
		if err = rows.Scan(&id, &created, &lastLogin, &enabled); err != nil {
			return err
		}
		fields := byID[id]
		fields["created_at"] = nil
		fields["last_login_at"] = nil
		if created != nil {
			fields["created_at"] = created.UTC().Format(time.RFC3339Nano)
		}
		if lastLogin != nil {
			fields["last_login_at"] = lastLogin.UTC().Format(time.RFC3339Nano)
		}
		fields["totp_enabled"] = enabled
	}
	return rows.Err()
}
