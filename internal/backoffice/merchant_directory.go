package backoffice

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type MerchantDirectoryFilter struct {
	Column, Status, Sort            string
	MerchantID, GroupID, PromoterID *int64
	From, To                        time.Time
}

var merchantSearchColumns = map[string]string{
	"uid":            "merchants.id::text=$1",
	"name":           "strpos(lower(merchants.name),lower($1))>0",
	"email":          "jsonb_typeof(merchants.profile->'email')='string' AND strpos(lower(merchants.profile->>'email'),lower($1))>0",
	"phone":          "jsonb_typeof(merchants.profile->'phone')='string' AND strpos(merchants.profile->>'phone',$1)>0",
	"qq":             "jsonb_typeof(merchants.profile->'qq')='string' AND strpos(merchants.profile->>'qq',$1)>0",
	"url":            "jsonb_typeof(merchants.profile->'url')='string' AND strpos(lower(merchants.profile->>'url'),lower($1))>0",
	"login_username": "EXISTS(SELECT 1 FROM console_users u WHERE u.role='merchant' AND u.subject_id=merchants.id AND strpos(lower(u.username),lower($1))>0)",
	"domain":         "EXISTS(SELECT 1 FROM backoffice_records d WHERE d.resource='domains' AND d.owner_id=merchants.id AND d.body->>'enabled'='true' AND d.body->>'approved'='true' AND strpos(lower(d.body->>'domain'),lower($1))>0)",
}

const merchantLastLoginSQL = "(SELECT max(l.created_at) FROM console_login_audit l WHERE l.role='merchant' AND l.success AND l.subject_id=merchants.id)"

var merchantDirectorySort = map[string]string{
	"": "merchants.id DESC", "id_desc": "merchants.id DESC",
	"money_desc":    "console_balances.available_minor::numeric DESC,merchants.id DESC",
	"money_asc":     "console_balances.available_minor::numeric ASC,merchants.id DESC",
	"lasttime_desc": merchantLastLoginSQL + " DESC NULLS LAST,merchants.id DESC",
	"lasttime_asc":  merchantLastLoginSQL + " ASC NULLS LAST,merchants.id DESC",
}

func directoryID(value string, zero bool) (*int64, error) {
	if value == "" {
		return nil, nil
	}
	n, e := strconv.ParseInt(value, 10, 64)
	if e != nil || n < 0 || (!zero && n == 0) || strconv.FormatInt(n, 10) != value {
		return nil, ErrInvalid
	}
	return &n, nil
}

func ParseMerchantDirectoryQuery(v url.Values) (*MerchantDirectoryFilter, string, error) {
	allowed := map[string]bool{"page": true, "limit": true, "q": true, "value": true, "column": true, "dstatus": true, "pay": true, "uid": true, "gid": true, "upid": true, "from": true, "to": true, "order": true}
	for k, values := range v {
		if !allowed[k] || len(values) != 1 || !utf8.ValidString(values[0]) || strings.ContainsAny(values[0], "\x00\r\n") {
			return nil, "", ErrInvalid
		}
	}
	for key, max := range map[string]int{"page": 100000, "limit": 100} {
		if raw := v.Get(key); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > max || strconv.Itoa(n) != raw {
				return nil, "", ErrInvalid
			}
		}
	}
	search := v.Get("q")
	if raw, exists := v["value"]; exists {
		if v.Has("q") && raw[0] != search {
			return nil, "", ErrInvalid
		}
		search = raw[0]
	}
	if len(search) > 256 {
		return nil, "", ErrInvalid
	}
	f := &MerchantDirectoryFilter{Column: v.Get("column"), Status: v.Get("dstatus"), Sort: v.Get("order")}
	if f.Column != "" {
		if _, ok := merchantSearchColumns[f.Column]; !ok {
			return nil, "", ErrInvalid
		}
	}
	if f.Column == "uid" && search != "" {
		if _, e := directoryID(search, false); e != nil {
			return nil, "", e
		}
	}
	if raw := v.Get("pay"); raw != "" {
		if raw != "0" && raw != "1" && raw != "2" {
			return nil, "", ErrInvalid
		}
		if f.Status != "" && f.Status != "pay_"+raw {
			return nil, "", ErrInvalid
		}
		f.Status = "pay_" + raw
	}
	switch f.Status {
	case "", "pay_0", "pay_1", "pay_2", "status_0", "status_1", "settle_0", "settle_1":
	default:
		return nil, "", ErrInvalid
	}
	if _, ok := merchantDirectorySort[f.Sort]; !ok {
		return nil, "", ErrInvalid
	}
	var e error
	if f.MerchantID, e = directoryID(v.Get("uid"), false); e != nil {
		return nil, "", e
	}
	if f.GroupID, e = directoryID(v.Get("gid"), true); e != nil {
		return nil, "", e
	}
	if f.PromoterID, e = directoryID(v.Get("upid"), false); e != nil {
		return nil, "", e
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	for key, dest := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
		if raw := v.Get(key); raw != "" {
			*dest, e = time.ParseInLocation("2006-01-02", raw, zone)
			if e != nil || dest.Year() < 1 || dest.Format("2006-01-02") != raw {
				return nil, "", ErrInvalid
			}
		}
	}
	if !f.From.IsZero() && !f.To.IsZero() && f.From.After(f.To) {
		return nil, "", ErrInvalid
	}
	return f, search, nil
}

func merchantDirectorySQL(f *MerchantDirectoryFilter, search string) (string, string, []any, error) {
	where := " WHERE ($1::text='' OR strpos(lower((" + merchantConsoleProjection + ")::text),lower($1))>0)"
	order := merchantDirectorySort[""]
	args := []any{search}
	if f == nil {
		return where, order, args, nil
	}
	if f.Column != "" {
		expr, ok := merchantSearchColumns[f.Column]
		if !ok {
			return "", "", nil, ErrInvalid
		}
		where = " WHERE ($1::text='' OR (" + expr + "))"
	}
	var ok bool
	if order, ok = merchantDirectorySort[f.Sort]; !ok {
		return "", "", nil, ErrInvalid
	}
	bind := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	switch f.Status {
	case "":
	case "pay_0", "pay_1", "pay_2":
		where += " AND merchants.pay=" + bind(int(f.Status[len(f.Status)-1]-'0')) + "::smallint"
	case "status_0":
		where += " AND NOT merchants.enabled"
	case "status_1":
		where += " AND merchants.enabled"
	case "settle_0", "settle_1":
		where += " AND merchant_settlement_permission(merchants.profile)=" + bind(f.Status[len(f.Status)-1:]) + "::text"
	default:
		return "", "", nil, ErrInvalid
	}
	if f.MerchantID != nil {
		where += " AND merchants.id=" + bind(*f.MerchantID) + "::bigint"
	}
	if f.GroupID != nil {
		where += " AND console_effective_group.group_id=" + bind(*f.GroupID) + "::bigint"
	}
	if f.PromoterID != nil {
		where += " AND EXISTS(SELECT 1 FROM referral_relations rel WHERE rel.invited_id=merchants.id AND rel.promoter_id=" + bind(*f.PromoterID) + "::bigint)"
	}
	if !f.From.IsZero() || !f.To.IsZero() {
		where += " AND merchants.registration_time_known"
	}
	if !f.From.IsZero() {
		where += " AND merchants.created_at>=" + bind(f.From) + "::timestamptz"
	}
	if !f.To.IsZero() {
		where += " AND merchants.created_at<" + bind(f.To.AddDate(0, 0, 1)) + "::timestamptz"
	}
	return where, order, args, nil
}
