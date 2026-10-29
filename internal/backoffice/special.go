package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"regexp"
	"strconv"
)

func (s *Service) createMerchant(ctx context.Context, p Principal, d definition, in map[string]json.RawMessage) (Item, error) {
	v, e := validate(p, d, in, nil, true)
	if e != nil {
		return Item{}, e
	}
	if e = validateMerchantOptions(v); e != nil {
		return Item{}, e
	}
	if e = validateMerchantContacts(v, in); e != nil {
		return Item{}, e
	}
	id, e := strconv.ParseInt(fmt.Sprint(v["id"]), 10, 64)
	if e != nil || id <= 0 {
		return Item{}, ErrInvalid
	}
	key, _ := v["api_key"].(string)
	if len(key) < 24 {
		return Item{}, fmt.Errorf("%w: API key minimum 24 bytes", ErrInvalid)
	}
	algorithm, _ := v["signature_algorithm"].(string)
	public, _ := v["rsa_public_key"].(string)
	if algorithm == "RSA" {
		if _, e = compat.ParsePublicKey(public); e != nil {
			return Item{}, ErrInvalid
		}
	}
	m := payment.Merchant{ID: id, Name: fmt.Sprint(v["name"]), APIKey: key, SignatureAlgorithm: algorithm, RSAPublicKey: public, Enabled: true}
	profile := merchantProfile(v)
	changes, _, e := groupAssignmentAudit(v, nil, in)
	if e != nil {
		return Item{}, e
	}
	e = s.store.CreateMerchantAtomic(ctx, m, func(ctx context.Context, tx pgx.Tx) error {
		if e := updateMerchantPay(ctx, tx, p, id, in, v); e != nil {
			return e
		}
		if e := validateGroup(ctx, tx, profile); e != nil {
			return e
		}
		raw, _ := json.Marshal(profile)
		enabled := true
		if b, ok := v["enabled"].(bool); ok {
			enabled = b
		}
		if _, e := tx.Exec(ctx, "UPDATE merchants SET enabled=$2,profile=$3::jsonb WHERE id=$1", id, enabled, string(raw)); e != nil {
			return e
		}
		if e := syncMerchantLogin(ctx, tx, id, v, false); e != nil {
			return e
		}
		return audit(ctx, tx, p, "merchants", id, "create", changes)
	})
	if e != nil {
		return Item{}, businessError(e)
	}
	return s.getSpecial(ctx, p, "merchants", id)
}
func (s *Service) updateMerchant(ctx context.Context, p Principal, d definition, id int64, in map[string]json.RawMessage) (Item, error) {
	m, e := s.store.Merchant(ctx, id)
	if e != nil {
		return Item{}, e
	}
	wasEnabled := m.Enabled

	current, e := s.getSpecial(ctx, p, "merchants", id)
	if e != nil {
		return Item{}, e
	}
	v, e := validate(p, d, in, current.Fields, false)
	if e != nil {
		return Item{}, e
	}
	if e = validateMerchantOptions(v); e != nil {
		return Item{}, e
	}
	if e = validateMerchantContacts(v, in); e != nil {
		return Item{}, e
	}
	m.Name = fmt.Sprint(v["name"])
	m.Enabled = v["enabled"].(bool)
	m.SignatureAlgorithm = fmt.Sprint(v["signature_algorithm"])
	if raw, ok := in["api_key"]; ok {
		var key string
		if json.Unmarshal(raw, &key) != nil || len(key) < 24 {
			return Item{}, ErrInvalid
		}
		m.APIKey = key
	}
	if raw, ok := in["rsa_public_key"]; ok {
		if json.Unmarshal(raw, &m.RSAPublicKey) != nil {
			return Item{}, ErrInvalid
		}
	}
	// Explicit policies are enforced from the locked stored row by Store.
	// Only the legacy NULL mode still derives availability from this algorithm.
	if m.Keytype == nil && m.SignatureAlgorithm == "RSA" {
		if _, e = compat.ParsePublicKey(m.RSAPublicKey); e != nil {
			return Item{}, ErrInvalid
		}
	}
	profile := merchantProfile(v)
	changes, confirmed, e := groupAssignmentAudit(v, current.Fields, in)
	if e != nil {
		return Item{}, e
	}
	raw, _ := json.Marshal(profile)
	// The public projection is a field allowlist, not the complete stored
	// profile. Preserve private migration metadata without returning/searching
	// it, and remove derived fields rather than persisting their display values.
	derived := []string{"pay"}
	for _, field := range d.fields {
		if field.ReadOnly {
			derived = append(derived, field.Name)
		}
	}
	e = s.pool.QueryRow(ctx, "SELECT (profile||$3::jsonb)-$4::text[] FROM merchants WHERE id=$1 AND config_revision=$2", id, m.ConfigRevision, string(raw), derived).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return Item{}, ErrConflict
	}
	if e != nil {
		return Item{}, e
	}
	e = s.store.ReplaceMerchantAtomic(ctx, m, raw, func(ctx context.Context, tx pgx.Tx) error {
		if e := updateMerchantPay(ctx, tx, p, id, in, v); e != nil {
			return e
		}
		if e := validateGroup(ctx, tx, profile); e != nil {
			return e
		}
		if confirmed {
			if e := confirmGroupAssignment(ctx, tx, id, profile); e != nil {
				return e
			}
		}
		if e := syncMerchantLogin(ctx, tx, id, v, m.Enabled != wasEnabled); e != nil {
			return e
		}
		return audit(ctx, tx, p, "merchants", id, "update", changes)
	})
	if e != nil {
		return Item{}, businessError(e)
	}
	return s.getSpecial(ctx, p, "merchants", id)
}
func (s *Service) getSpecial(ctx context.Context, p Principal, r string, id int64) (Item, error) {
	var raw []byte
	var e error
	if r == "merchants" {
		e = s.pool.QueryRow(ctx, "SELECT "+merchantConsoleProjection+" FROM "+merchantConsoleFrom+" WHERE merchants.id=$1", id).Scan(&raw)
	} else {
		e = s.pool.QueryRow(ctx, "SELECT jsonb_build_object('username',username,'display_name',display_name,'enabled',enabled,'admin_class',admin_class,'permissions',permissions,'auth_version',auth_version::text) FROM console_users WHERE id=$1 AND role='admin'", id).Scan(&raw)
	}
	if e != nil {
		return Item{}, businessError(e)
	}
	item := Item{ID: id, Fields: decodeFields(raw)}
	if r == "merchants" {
		if e = s.attachMerchantRecipients(ctx, []Item{item}); e != nil {
			return Item{}, e
		}
	}
	return item, nil
}
func (s *Service) listSpecial(ctx context.Context, p Principal, r string, q Query) (Page, error) {
	page := Page{Items: []Item{}, Page: q.Page, Limit: q.Limit}
	table, condition, projection := merchantConsoleFrom, "true", merchantConsoleProjection
	idColumn := "merchants.id"
	if r == "admin-users" {
		table = "console_users"
		condition = "role='admin'"
		projection = "jsonb_build_object('username',username,'display_name',display_name,'enabled',enabled,'admin_class',admin_class,'permissions',permissions)"
		idColumn = "console_users.id"
	}
	where := " WHERE " + condition + " AND ($1::text='' OR strpos(lower((" + projection + ")::text),lower($1))>0)"
	order := idColumn + " DESC"
	args := []any{q.Search}
	if r == "merchants" {
		var e error
		where, order, args, e = merchantDirectorySQL(q.Merchant, q.Search)
		if e != nil {
			return page, e
		}
	}
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+table+where, args...).Scan(&page.Total); e != nil {
		return page, e
	}
	limitIndex := len(args) + 1
	args = append(args, q.Limit, (q.Page-1)*q.Limit)
	rows, e := s.pool.Query(ctx, "SELECT "+idColumn+","+projection+" FROM "+table+where+" ORDER BY "+order+fmt.Sprintf(" LIMIT $%d OFFSET $%d", limitIndex, limitIndex+1), args...)
	if e != nil {
		return page, e
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			return page, e
		}
		page.Items = append(page.Items, Item{ID: id, Fields: decodeFields(raw)})
	}
	if e = rows.Err(); e != nil {
		return page, e
	}
	// Release the list cursor before the one bounded summary query, including
	// installations with a one-connection pool.
	rows.Close()
	if r == "merchants" {
		e = s.attachMerchantRecipients(ctx, page.Items)
	}
	return page, e
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.@-]{3,100}$`)

func (s *Service) createAdministrator(ctx context.Context, p Principal, d definition, in map[string]json.RawMessage) (Item, error) {
	permissions, hasPermissions, e := extractAdminPermissions(in)
	if e != nil {
		return Item{}, e
	}
	v, e := validate(p, d, in, nil, true)
	if e != nil {
		return Item{}, e
	}
	username := fmt.Sprint(v["username"])
	if !usernamePattern.MatchString(username) {
		return Item{}, ErrInvalid
	}
	// Per-menu permissions only narrow an operator; super ignores them. Reject
	// an attempt to pin a grant set onto a super row so the stored value never
	// contradicts the enforced class.
	if fmt.Sprint(v["admin_class"]) == "super" && hasPermissions && len(permissions) > 0 {
		return Item{}, ErrInvalid
	}
	password, _ := v["password"].(string)
	hash, e := identity.HashPassword(password)
	if e != nil {
		return Item{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Item{}, e
	}
	defer tx.Rollback(ctx)
	var id int64
	enabled, _ := v["enabled"].(bool)
	permissionArg := adminPermissionArg(permissions, hasPermissions)
	e = tx.QueryRow(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,enabled,admin_class,permissions) VALUES($1,'admin',nextval('console_subject_seq'),$2,$3,$4,$5,$6) RETURNING id", username, fmt.Sprint(v["display_name"]), hash, enabled, v["admin_class"], permissionArg).Scan(&id)
	if e != nil {
		return Item{}, businessError(e)
	}
	if hasPermissions {
		v["permissions"] = permissions
	}
	if e = audit(ctx, tx, p, "admin-users", id, "create", v); e != nil {
		return Item{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Item{}, e
	}
	delete(v, "password")
	return Item{ID: id, Fields: v}, nil
}
func (s *Service) updateAdministrator(ctx context.Context, p Principal, d definition, id int64, in map[string]json.RawMessage) (Item, error) {
	if _, ok := in["username"]; ok {
		return Item{}, ErrForbidden
	}
	permissions, hasPermissions, e := extractAdminPermissions(in)
	if e != nil {
		return Item{}, e
	}
	current, e := s.getSpecial(ctx, p, "admin-users", id)
	if e != nil {
		return Item{}, e
	}
	v, e := validate(p, d, in, current.Fields, false)
	if e != nil {
		return Item{}, e
	}
	if v["enabled"] == false && current.Fields["username"] == p.Username {
		return Item{}, ErrForbidden
	}
	// The resulting class decides whether a grant set is meaningful: forbid
	// storing permissions on a row that is (or becomes) super.
	if fmt.Sprint(v["admin_class"]) == "super" && hasPermissions && len(permissions) > 0 {
		return Item{}, ErrInvalid
	}
	hash := ""
	if raw, ok := in["password"]; ok {
		var password string
		if json.Unmarshal(raw, &password) != nil {
			return Item{}, ErrInvalid
		}
		hash, e = identity.HashPassword(password)
		if e != nil {
			return Item{}, ErrInvalid
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Item{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973232)"); e != nil {
		return Item{}, e
	}
	if v["enabled"] == false {
		var count int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM console_users WHERE role='admin' AND enabled AND id<>$1", id).Scan(&count); e != nil {
			return Item{}, e
		}
		if count < 1 {
			return Item{}, ErrForbidden
		}
	}
	if current.Fields["admin_class"] == "super" && (v["enabled"] == false || v["admin_class"] != "super") {
		if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973234)"); e != nil {
			return Item{}, e
		}
		var other bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM console_users WHERE role='admin' AND admin_class='super' AND enabled AND id<>$1)", id).Scan(&other); e != nil {
			return Item{}, e
		}
		if !other {
			return Item{}, ErrForbidden
		}
	}
	// permissions is updated only when the caller sent the field. When the row
	// is (or becomes) super the stored set is cleared so an operator demotion is
	// not silently granted a stale super's empty set.
	setPermissions := hasPermissions
	permissionArg := adminPermissionArg(permissions, hasPermissions)
	if fmt.Sprint(v["admin_class"]) == "super" {
		setPermissions = true
		permissionArg = nil
	}
	tag, e := tx.Exec(ctx, "UPDATE console_users SET display_name=$2,enabled=$3,password_hash=CASE WHEN $4::text='' THEN password_hash ELSE $4 END,admin_class=$5,permissions=CASE WHEN $7::boolean THEN $8::jsonb ELSE permissions END WHERE id=$1 AND role='admin' AND auth_version=$6::bigint", id, fmt.Sprint(v["display_name"]), v["enabled"], hash, v["admin_class"], fmt.Sprint(current.Fields["auth_version"]), setPermissions, permissionArg)
	if e != nil {
		return Item{}, e
	}
	if tag.RowsAffected() != 1 {
		return Item{}, ErrConflict
	}
	if hasPermissions {
		v["permissions"] = permissions
	}
	if e = audit(ctx, tx, p, "admin-users", id, "update", v); e != nil {
		return Item{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Item{}, e
	}
	delete(v, "password")
	return Item{ID: id, Fields: v}, nil
}

// extractAdminPermissions removes the permissions field from in (so the generic
// validator never sees the array-typed value) and returns the catalog-validated
// key set, whether the field was present, and any validation error.
func extractAdminPermissions(in map[string]json.RawMessage) ([]string, bool, error) {
	raw, ok := in["permissions"]
	if !ok {
		return nil, false, nil
	}
	delete(in, "permissions")
	var keys []string
	if json.Unmarshal(raw, &keys) != nil {
		return nil, true, ErrInvalid
	}
	normalized, valid := normalizeAdminPermissions(keys)
	if !valid {
		return nil, true, ErrInvalid
	}
	return normalized, true, nil
}

// adminPermissionArg renders the permissions grant for the jsonb column: NULL
// when the field was not supplied or the set is empty (unrestricted operator),
// otherwise the JSON-encoded key array.
func adminPermissionArg(permissions []string, has bool) any {
	if !has || len(permissions) == 0 {
		return nil
	}
	b, _ := json.Marshal(permissions)
	return string(b)
}
