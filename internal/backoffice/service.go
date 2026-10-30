package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"goepay/internal/businessnotice"
	"goepay/internal/collectionstyle"
	"goepay/internal/ledger"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/payment"
	"goepay/internal/siteinfo"
)

var (
	ErrNotFound  = errors.New("resource not found")
	ErrForbidden = errors.New("operation forbidden")
	ErrInvalid   = errors.New("invalid resource input")
	ErrConflict  = errors.New("resource conflict")
)

type Principal struct {
	Role       string
	SubjectID  int64
	Username   string
	AdminClass string
}
type Query struct {
	Page, Limit int
	Search      string
	Merchant    *MerchantDirectoryFilter
	// Filter holds validated field=value equality filters (keys are a
	// definition's declared FilterSpec Params only; List rejects any other key).
	Filter map[string]string
}
type Item struct {
	ID     int64
	Fields map[string]any
}
type Page struct {
	Items       []Item
	Total       int64
	Page, Limit int
}
type Service struct {
	// Supplied by the host from its actual configured providers, never the
	// unrelated editable channel directory or a request-supplied identity.
	GroupChannelAllowed  func(int64, string) bool
	GroupRoutingChannels func() ([]GroupPaymentChannel, error)
	RechargeValidate     func(context.Context, int64, map[string]string) error
	RechargeStart        func(context.Context, int64, string, map[string]string) (string, error)
	pool                 *pgxpool.Pool
	store                *payment.Store
}

func New(pool *pgxpool.Pool, store *payment.Store) *Service {
	return &Service{pool: pool, store: store}
}
func authorized(p Principal, d definition, write bool) bool {
	if d.superOnly && (p.Role != "admin" || p.AdminClass != "super") {
		return false
	}
	return p.SubjectID > 0 && (p.Role == "admin" || p.Role == "merchant" && (d.tenant && (!write || !d.merchantReadOnly) || !write && d.merchantRead))
}
func definitionFor(p Principal, r string, write bool) (definition, error) {
	// Legacy plaintext settings are retained for historical inspection only.
	// New recipient versions use the dedicated encrypted workflow.
	if (r == "settlement-settings" || r == "template-configs" || r == "cron-tasks") && write {
		return definition{}, ErrForbidden
	}
	d, ok := definitions[r]
	if !ok {
		return d, ErrNotFound
	}
	if !authorized(p, d, write) {
		return d, ErrForbidden
	}
	return d, nil
}
func businessError(e error) error {
	var pe *pgconn.PgError
	if errors.As(e, &pe) {
		switch pe.Code {
		case "23505":
			return ErrConflict
		case "23503", "23514", "22P02":
			return ErrInvalid
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return e
}

var (
	domainPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	colorPattern  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	codePattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,64}$`)
)

func validate(p Principal, d definition, raw map[string]json.RawMessage, current map[string]any, create bool) (map[string]any, error) {
	d = layoutSettingDefinition(d, raw, current)
	d = siteSettingDefinition191(d, raw, current)
	fields := map[string]Field{}
	for _, f := range d.fields {
		fields[f.Name] = f
	}
	out := map[string]any{}
	for k, v := range current {
		out[k] = v
	}
	for k, v := range raw {
		f, ok := fields[k]
		if !ok {
			return nil, fmt.Errorf("%w: field %s", ErrInvalid, k)
		}
		if f.AdminOnly && p.Role != "admin" {
			return nil, ErrForbidden
		}
		if f.ReadOnly || f.CreateOnly && !create {
			return nil, ErrForbidden
		}
		var x any
		if e := json.Unmarshal(v, &x); e != nil {
			return nil, ErrInvalid
		}
		switch f.Type {
		case "json":
			if len(v) > f.MaxLength && f.MaxLength > 0 {
				return nil, ErrInvalid
			}
			if encoded, ok := x.(string); ok {
				if json.Unmarshal([]byte(encoded), &x) != nil {
					return nil, ErrInvalid
				}
			}
			switch x.(type) {
			case map[string]any, []any:
			default:
				return nil, ErrInvalid
			}
			if hasSecret(x) {
				return nil, ErrInvalid
			}
		case "boolean":
			if _, ok = x.(bool); !ok {
				return nil, ErrInvalid
			}
		case "integer":
			var s string
			switch n := x.(type) {
			case string:
				s = n
			case float64:
				s = string(v)
			default:
				return nil, ErrInvalid
			}
			n, e := strconv.ParseInt(s, 10, 64)
			if e != nil || f.Min != nil && n < *f.Min || f.Max != nil && n > *f.Max {
				return nil, ErrInvalid
			}
			x = strconv.FormatInt(n, 10)
		default:
			s, ok := x.(string)
			if !ok || f.MaxLength > 0 && len(s) > f.MaxLength {
				return nil, ErrInvalid
			}
			if f.Type != "password" {
				s = strings.TrimSpace(s)
			}
			if f.Type == "select" {
				found := false
				for _, o := range f.Options {
					if s == o.Value {
						found = true
					}
				}
				if !found {
					return nil, ErrInvalid
				}
			}
			if f.Type == "email" && s != "" {
				address, e := mail.ParseAddress(s)
				if e != nil || address.Address != s {
					return nil, ErrInvalid
				}
			}
			if f.Type == "url" && s != "" {
				u, e := url.Parse(s)
				if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
					return nil, ErrInvalid
				}
			}
			x = s
		}
		out[k] = x
	}
	for _, f := range d.fields {
		if _, ok := out[f.Name]; !ok && create && f.Default != nil {
			out[f.Name] = f.Default
		}
		if f.Required {
			v, ok := out[f.Name]
			if !ok || v == "" {
				return nil, fmt.Errorf("%w: missing %s", ErrInvalid, f.Name)
			}
		}
	}
	return out, nil
}
func validateSpecific(resource string, v map[string]any) error {
	switch resource {
	case "announcements":
		return validateAnnouncement(v)
	case "groups":
		if err := validateGroupPaymentRates(v); err != nil {
			return err
		}
		if err := validateOriginalPaymentLimits(v); err != nil {
			return err
		}
		if raw, ok := v["settle_rate"]; ok && raw != nil && fmt.Sprint(raw) != "" {
			n, e := ledger.Parse(fmt.Sprint(raw))
			if e != nil || n < 0 || n > 10000 {
				return ErrInvalid
			}
		}
	case "order-subject-policies":
		return validateSubjectPolicy(v)
	case "pay-rolls":
		_, e := ParsePayRoll(v)
		return e
	case "domains":
		domain, _ := v["domain"].(string)
		domain, valid := normalizeAuthorizationDomain(domain)
		if !valid {
			return ErrInvalid
		}
		v["domain"] = domain
	case "invitation-codes":
		if !codePattern.MatchString(fmt.Sprint(v["code"])) {
			return ErrInvalid
		}
		if v["max_uses"] == nil {
			v["max_uses"] = "1"
		}
	case "settings":
		key := fmt.Sprint(v["key"])
		if originalLayoutSetting(key) {
			if _, ok := v["value"].(string); !ok {
				return ErrInvalid
			}
		}
		value := fmt.Sprint(v["value"])
		if validatePublicTestPaySetting203(key, value) != nil {
			return ErrInvalid
		}
		if known, err := validateCombineSetting207(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateMerchantTransferSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateSettleTransferChannelSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validatePayGuardSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateMerchantFeatureSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateDepositSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateMerchantSwitchSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateProxySetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateOAuthLoginSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := validateRedpackSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := siteinfo.Validate(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := notice.ValidatePlatformSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		if known, err := businessnotice.ValidatePlatformSetting(key, value); known && err != nil {
			return ErrInvalid
		}
		switch key {
		case "auto_check_notify", "check_notify_count", "check_notify_notice":
			if payment.ValidateScheduledNotificationSetting(key, value) != nil {
				return ErrInvalid
			}
		case "msgconfig_risk", "msgtelegram_chatid", "qqbot_openid", "mail_recv", "mail_name":
			if payment.ValidateAdminNoticeSetting(key, value) != nil {
				return ErrInvalid
			}
		case "auto_settle_money":
			n, e := ledger.Parse(value)
			if e != nil || n < 0 || n > 100000000000 {
				return ErrInvalid
			}
		case "transfer_alipay", "test_pay_uid":
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil || n < 0 || strconv.FormatInt(n, 10) != value {
				return ErrInvalid
			}
		case "registration_open", "registration_invite_required", "enforce_domains", "payment_fee_enabled", "transfer_enabled", "admin_funds_authorization_enabled":
			if value != "true" && value != "false" {
				return ErrInvalid
			}
		case "user_review":
			if value != "0" && value != "1" {
				return ErrInvalid
			}
		case "refund_fee_payer":
			if value != "platform" && value != "merchant" {
				return ErrInvalid
			}
		case "settle_type":
			if value != "0" && value != "1" {
				return ErrInvalid
			}
		case "settle_open":
			if value != "0" && value != "1" && value != "2" && value != "3" {
				return ErrInvalid
			}
		case "pay_minmoney", "pay_maxmoney", "transfer_minmoney", "transfer_maxmoney":
			if _, e := ledger.Parse(value); e != nil {
				return ErrInvalid
			}
		case "transfer_maxlimit":
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil || n < 0 || n > 1000000 {
				return ErrInvalid
			}
		case "transfer_desc", "transfer_name":
			if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 32 || strings.ContainsAny(value, "\x00\r\n") {
				return ErrInvalid
			}
		case "settlement_minimum_cents", "settlement_maximum_cents", "settlement_fee_minimum_cents", "settlement_fee_maximum_cents", "settlement_retained_cents":
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil || n < 0 || n > 100000000000 {
				return ErrInvalid
			}
		case "settlement_fee_rate_bps", "settlement_daily_limit":
			n, e := strconv.ParseInt(value, 10, 64)
			max := int64(1000000)
			if key == "settlement_fee_rate_bps" {
				max = 10000
			}
			if e != nil || n < 0 || n > max {
				return ErrInvalid
			}
		case "support_email":
			if _, e := mail.ParseAddress(value); e != nil {
				return ErrInvalid
			}
		}
	case "blacklist":
		value := fmt.Sprint(v["value"])
		switch v["kind"] {
		case "ip":
			if net.ParseIP(value) == nil {
				if _, _, e := net.ParseCIDR(value); e != nil {
					return ErrInvalid
				}
			}
		case "domain":
			if !domainPattern.MatchString(strings.ToLower(value)) {
				return ErrInvalid
			}
			v["value"] = strings.ToLower(value)
		}
	case "collection-styles":
		if collectionstyle.IsLayout(v) {
			if _, e := collectionstyle.Parse(v); e != nil {
				return ErrInvalid
			}
		} else if fmt.Sprint(v["title"]) == "" || !colorPattern.MatchString(fmt.Sprint(v["background_color"])) || !colorPattern.MatchString(fmt.Sprint(v["text_color"])) {
			return ErrInvalid
		}
	}
	return nil
}
func owner(p Principal, d definition, v map[string]any) (int64, error) {
	if !d.tenant {
		return 0, nil
	}
	if p.Role == "merchant" {
		v["merchant_id"] = strconv.FormatInt(p.SubjectID, 10)
		return p.SubjectID, nil
	}
	id, e := strconv.ParseInt(fmt.Sprint(v["merchant_id"]), 10, 64)
	if e != nil || id <= 0 {
		return 0, ErrInvalid
	}
	return id, nil
}
func audit(ctx context.Context, tx pgx.Tx, p Principal, r string, id int64, operation string, changes map[string]any) error {
	safe := map[string]any{}
	for k, v := range changes {
		if !strings.Contains(strings.ToLower(k), "password") && k != "api_key" && k != "rsa_public_key" && k != "account_reference" && k != "account_name" && k != "contact_phone" {
			safe[k] = v
		} else {
			safe[k] = "[changed]"
		}
	}
	raw, e := json.Marshal(safe)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)", p.Role, p.SubjectID, p.Username, r, id, operation, string(raw))
	return e
}
func (s *Service) Create(ctx context.Context, p Principal, r string, in map[string]json.RawMessage) (Item, error) {
	d, e := definitionFor(p, r, true)
	if e != nil {
		return Item{}, e
	}
	if r == "merchants" {
		return s.createMerchant(ctx, p, d, in)
	}
	if r == "admin-users" {
		return s.createAdministrator(ctx, p, d, in)
	}
	if r == "settings" {
		if e = rejectPlatformSetting179(in, nil); e != nil {
			return Item{}, e
		}
		if e = validateMerchantNoticeRaw(in, nil); e != nil {
			return Item{}, e
		}
	}
	if r == "groups" {
		if in, e = normalizeGroupPaymentInfo(in); e != nil {
			return Item{}, e
		}
	}
	v, e := validate(p, d, in, nil, true)
	if e != nil {
		return Item{}, e
	}
	if e = validateSpecific(r, v); e != nil {
		return Item{}, e
	}
	ownerID, e := owner(p, d, v)
	if e != nil {
		return Item{}, e
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return Item{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Item{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockPaymentLimitSettings(ctx, tx, r); e != nil {
		return Item{}, e
	}
	if e = validatePaymentLimitSetting(ctx, tx, r, 0, v); e != nil {
		return Item{}, e
	}
	if r == "groups" {
		if e = s.validateGroupRoutingReferences(ctx, tx, v); e != nil {
			return Item{}, e
		}
	}
	if e = validateRelations(ctx, tx, r, 0, ownerID, v); e != nil {
		return Item{}, e
	}
	raw, e = json.Marshal(v)
	if e != nil {
		return Item{}, e
	}
	var id int64
	var own any
	if ownerID != 0 {
		own = ownerID
	}
	e = tx.QueryRow(ctx, "INSERT INTO backoffice_records(resource,owner_id,body) VALUES($1,$2,$3::jsonb) RETURNING id", r, own, string(raw)).Scan(&id)
	if e != nil {
		return Item{}, businessError(e)
	}
	submissionAudited := false
	if r == "domains" {
		submissionAudited, e = s.auditDomainSubmission(ctx, tx, p, id, "create", v)
		if e != nil {
			return Item{}, e
		}
	}
	if !submissionAudited {
		if e = audit(ctx, tx, p, r, id, "create", v); e != nil {
			return Item{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Item{}, businessError(e)
	}
	return Item{ID: id, Fields: v}, nil
}
func (s *Service) Get(ctx context.Context, p Principal, r string, id int64) (Item, error) {
	if _, e := definitionFor(p, r, false); e != nil {
		return Item{}, e
	}
	if r == "merchants" || r == "admin-users" {
		return s.getSpecial(ctx, p, r, id)
	}
	var raw []byte
	e := s.pool.QueryRow(ctx, "SELECT body FROM backoffice_records WHERE resource=$1 AND id=$2 AND ($3::text='admin' OR owner_id=$4 OR $1='groups')", r, id, p.Role, p.SubjectID).Scan(&raw)
	if e != nil {
		return Item{}, businessError(e)
	}
	fields, e := decodeResourceFields(r, raw)
	if e != nil {
		return Item{}, e
	}
	if r == "settings" && fields["key"] == "__platform_merchant_access179" {
		return Item{}, ErrNotFound
	}
	item := Item{ID: id, Fields: fields}
	if r == "groups" {
		if e = s.attachGroupCoreRisk(ctx, []Item{item}); e != nil {
			return Item{}, e
		}
	}
	return item, nil
}
func (s *Service) List(ctx context.Context, p Principal, r string, q Query) (Page, error) {
	d, e := definitionFor(p, r, false)
	if e != nil {
		return Page{}, e
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	if q.Page < 1 || q.Page > 100000 || q.Limit < 1 || q.Limit > 100 || len(q.Search) > 256 {
		return Page{}, ErrInvalid
	}
	if r == "merchants" || r == "admin-users" {
		return s.listSpecial(ctx, p, r, q)
	}
	if r == "pay-types" {
		// The 7 original pre_type rows always show with their effective 状态,
		// merged with the stored records (the original console shows the whole
		// catalog on one page), so an administrator can open a type that has no
		// record yet. authorized() above already restricted this to admins.
		return s.listPayTypes(ctx, p)
	}
	where := "resource=$1 AND ($2::text='admin' OR owner_id=$3 OR $4::boolean) AND ($5::text='' OR strpos(lower(body::text),lower($5))>0)"
	if r == "settings" {
		where += " AND body->>'key' IS DISTINCT FROM '__platform_merchant_access179'"
	}
	if r == "invitation-codes" {
		where += " AND body->>'archived' IS DISTINCT FROM 'true'"
	}
	// Generic per-field toolbar filters (schema FilterSpec). Only a param
	// declared on this definition is accepted; its validated value is bound as a
	// parameter and compared against body->>'<param>' (booleans render 'true'/
	// 'false'). An unknown param is a client error.
	args := []any{r, p.Role, p.SubjectID, d.merchantRead, q.Search}
	for param, value := range q.Filter {
		if value == "" {
			continue
		}
		known := false
		for _, fs := range d.filters {
			if fs.Param == param {
				for _, o := range fs.Options {
					if o.Value == value {
						known = true
					}
				}
			}
		}
		if !known {
			return Page{}, ErrInvalid
		}
		args = append(args, value)
		where += " AND body->>'" + param + "'=$" + strconv.Itoa(len(args))
	}
	page := Page{Items: []Item{}, Page: q.Page, Limit: q.Limit}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_records WHERE "+where, args...).Scan(&page.Total); e != nil {
		return page, e
	}
	order := "id DESC"
	projection := "id,body"
	if r == "groups" {
		order = "CASE WHEN id=0 THEN 0 ELSE 1 END,id DESC"
	}
	if r == "announcements" {
		order = "CASE WHEN body->>'sort'~'^[1-9][0-9]{0,9}$' THEN (body->>'sort')::numeric ELSE 50 END ASC,id DESC"
		projection = "id,body || jsonb_build_object('created_at',created_at)"
	}
	if r == "blacklist" {
		// Pro shows 添加时间; surface the record's created_at into the body so the
		// readonly schema field renders it (does not change stored data).
		projection = "id,body || jsonb_build_object('created_at',created_at)"
	}
	if r == "domains" {
		// Pro's 授权域名 list shows 添加时间; surface created_at (read-only) the same
		// way. 审核时间 is not separately tracked in GOEPAY, so it is not fabricated.
		projection = "id,body || jsonb_build_object('created_at',created_at)"
	}
	limitPos := strconv.Itoa(len(args) + 1)
	offsetPos := strconv.Itoa(len(args) + 2)
	args = append(args, q.Limit, (q.Page-1)*q.Limit)
	rows, e := s.pool.Query(ctx, "SELECT "+projection+" FROM backoffice_records WHERE "+where+" ORDER BY "+order+" LIMIT $"+limitPos+" OFFSET $"+offsetPos, args...)
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
		fields, e := decodeResourceFields(r, raw)
		if e != nil {
			return Page{}, e
		}
		page.Items = append(page.Items, Item{ID: id, Fields: fields})
	}
	if e = rows.Err(); e != nil {
		return page, e
	}
	rows.Close()
	if r == "groups" {
		e = s.attachGroupCoreRisk(ctx, page.Items)
	}
	return page, e
}
func (s *Service) Update(ctx context.Context, p Principal, r string, id int64, in map[string]json.RawMessage) (Item, error) {
	return s.update(ctx, p, r, id, in, "")
}
func (s *Service) update(ctx context.Context, p Principal, r string, id int64, in map[string]json.RawMessage, expectedStyle string) (Item, error) {
	d, e := definitionFor(p, r, true)
	if e != nil {
		return Item{}, e
	}
	if r == "merchants" {
		return s.updateMerchant(ctx, p, d, id, in)
	}
	if r == "admin-users" {
		return s.updateAdministrator(ctx, p, d, id, in)
	}
	if r == "groups" {
		if in, e = normalizeGroupPaymentInfo(in); e != nil {
			return Item{}, e
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Item{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockPaymentLimitSettings(ctx, tx, r); e != nil {
		return Item{}, e
	}
	var raw []byte
	e = tx.QueryRow(ctx, "SELECT body FROM backoffice_records WHERE resource=$1 AND id=$2 AND ($3::text='admin' OR owner_id=$4) FOR UPDATE", r, id, p.Role, p.SubjectID).Scan(&raw)
	if e != nil {
		return Item{}, businessError(e)
	}
	if r == "onboarding-applications" && p.Role == "merchant" {
		state := fmt.Sprint(decodeFields(raw)["status"])
		if state != "draft" && state != "action_required" {
			return Item{}, ErrForbidden
		}
	}
	if r == "collection-styles" && (collectionstyle.IsLayout(decodeFields(raw)) || expectedStyle != "") && collectionstyle.Revision(decodeFields(raw)) != expectedStyle {
		return Item{}, ErrConflict
	}
	if r == "invitation-codes" && decodeFields(raw)["archived"] == true {
		return Item{}, ErrConflict
	}
	if r == "collection-codes" {
		var generated bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM collection_code_generations WHERE code_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='collection-generator' AND operation='generate' AND resource_id=$1)`, id).Scan(&generated); e != nil {
			return Item{}, e
		}
		if generated || decodeFields(raw)["generated"] == true {
			if decodeFields(raw)["archived"] == true {
				return Item{}, ErrConflict
			}
			for key := range in {
				if key != "enabled" {
					return Item{}, ErrForbidden
				}
			}
		}
	}
	if r == "settings" {
		if e = rejectPlatformSetting179(in, decodeFields(raw)); e != nil {
			return Item{}, e
		}
		if fmt.Sprint(decodeFields(raw)["key"]) == "group_buy" {
			return Item{}, ErrForbidden
		}
		if e = validateMerchantNoticeRaw(in, decodeFields(raw)); e != nil {
			return Item{}, e
		}
	}
	currentFields, e := decodeResourceFields(r, raw)
	if e != nil {
		return Item{}, e
	}
	v, e := validate(p, d, in, currentFields, false)
	if e != nil {
		return Item{}, e
	}
	if r == "collection-styles" && collectionstyle.IsLayout(v) && expectedStyle == "" {
		return Item{}, ErrConflict
	}
	if e = validateSpecific(r, v); e != nil {
		return Item{}, e
	}
	if e = validatePaymentLimitSetting(ctx, tx, r, id, v); e != nil {
		return Item{}, e
	}
	// Approval applies to the reviewed hostname, not to this mutable record ID.
	domainResubmitted := r == "domains" && p.Role == "merchant" && fmt.Sprint(v["domain"]) != fmt.Sprint(decodeFields(raw)["domain"])
	if domainResubmitted {
		v["approved"] = false
	}
	oid, e := owner(p, d, v)
	if e != nil {
		return Item{}, e
	}
	var own any
	if oid > 0 {
		own = oid
	}
	if _, changed := in["info"]; r == "groups" && changed {
		if e = s.validateGroupRoutingReferences(ctx, tx, v); e != nil {
			return Item{}, e
		}
	}
	if e = validateRelations(ctx, tx, r, id, oid, v); e != nil {
		return Item{}, e
	}
	raw, e = json.Marshal(v)
	if e != nil {
		return Item{}, e
	}
	_, e = tx.Exec(ctx, "UPDATE backoffice_records SET owner_id=$3,body=$4::jsonb,updated_at=now() WHERE resource=$1 AND id=$2", r, id, own, string(raw))
	if e != nil {
		return Item{}, businessError(e)
	}
	_, explicitDecision := in["approved"]
	decisionAudited := false
	if r == "domains" && p.Role == "admin" && explicitDecision {
		decisionAudited, e = s.auditDomainReview(ctx, tx, p, id, v)
		if e != nil {
			return Item{}, e
		}
	}
	if !decisionAudited {
		if domainResubmitted {
			decisionAudited, e = s.auditDomainSubmission(ctx, tx, p, id, "update", v)
			if e != nil {
				return Item{}, e
			}
		}
	}
	if !decisionAudited {
		if e = audit(ctx, tx, p, r, id, "update", v); e != nil {
			return Item{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Item{}, businessError(e)
	}
	return Item{ID: id, Fields: v}, nil
}
func (s *Service) Delete(ctx context.Context, p Principal, r string, id int64) error {
	if r == "groups" && id == 0 {
		return ErrForbidden
	}
	d, e := definitionFor(p, r, true)
	if e != nil {
		return e
	}
	if r == "admin-users" {
		// A super administrator may remove another admin (self-protection and
		// the last-super-admin invariant are enforced in deleteAdministrator and
		// the DB trigger). noDelete still blocks the generic delete path below.
		return s.deleteAdministrator(ctx, p, id)
	}
	if d.noDelete {
		return ErrForbidden
	}
	if r == "invitation-codes" {
		return s.archiveInvitation(ctx, p, id)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lockPaymentLimitSettings(ctx, tx, r); e != nil {
		return e
	}
	if r == "settings" {
		var key string
		if e = tx.QueryRow(ctx, `SELECT body->>'key' FROM backoffice_records WHERE resource='settings' AND id=$1 FOR UPDATE`, id).Scan(&key); e != nil {
			return businessError(e)
		}
		if protectedPlatformSetting179(key) {
			return ErrForbidden
		}
	}
	if r == "collection-codes" {
		var generated bool
		var raw []byte
		if e = tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource=$1 AND id=$2 AND ($3::text='admin' OR owner_id=$4) FOR UPDATE`, r, id, p.Role, p.SubjectID).Scan(&raw); e != nil {
			return businessError(e)
		}
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM collection_code_generations WHERE code_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='collection-generator' AND operation='generate' AND resource_id=$1)`, id).Scan(&generated); e != nil {
			return e
		}
		if generated || decodeFields(raw)["generated"] == true {
			if _, e = tx.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"enabled":false,"archived":true}'::jsonb,updated_at=now() WHERE id=$1`, id); e != nil {
				return e
			}
			if e = audit(ctx, tx, p, r, id, "archive", nil); e != nil {
				return e
			}
			return tx.Commit(ctx)
		}
	}
	tag, e := tx.Exec(ctx, "DELETE FROM backoffice_records WHERE resource=$1 AND id=$2 AND ($3::text='admin' OR owner_id=$4) AND NOT(resource='settings' AND body->>'key'='group_buy')", r, id, p.Role, p.SubjectID)
	if e != nil {
		return businessError(e)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if e = audit(ctx, tx, p, r, id, "delete", nil); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func hasSecret(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			n := strings.ToLower(k)
			if strings.Contains(n, "password") || strings.Contains(n, "secret") || strings.Contains(n, "private_key") || strings.Contains(n, "api_key") || strings.Contains(n, "token") {
				return true
			}
			if hasSecret(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if hasSecret(v) {
				return true
			}
		}
	}
	return false
}
