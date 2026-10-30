package backoffice

import "context"

// weixinAppsDefinition is the 公众号小程序 resource: the dialog keeps 名称 /
// App ID / 类别 / 启用; 类别 carries the original Chinese names (公众号 /
// 小程序) while its value enum stays official_account / mini_program so
// validation and routing are unchanged.
func weixinAppsDefinition() definition {
	kind := selectField("kind", "类别", "official_account", "mini_program")
	kindNames := map[string]string{"official_account": "公众号", "mini_program": "小程序"}
	for i := range kind.Options {
		kind.Options[i].Label = kindNames[kind.Options[i].Value]
	}
	// Pro's 公众号小程序 filter card carries a 类别 select (全部类别 / 公众号 /
	// 小程序) beside the 名称/APPID search. Surface it as a generic toolbar
	// filter on the stored kind field; the value enum and routing are unchanged.
	kindFilter := FilterSpec{Param: "kind", Label: "类别", Options: []Option{
		{Label: "全部类别", Value: ""},
		{Label: "公众号", Value: "official_account"},
		{Label: "小程序", Value: "mini_program"},
	}}
	return definition{title: "公众号小程序", description: "保存公开应用标识；密钥通过受控配置加载。", filters: []FilterSpec{kindFilter}, fields: []Field{text("name", "名称", true, 100), text("app_id", "App ID", true, 100), kind, flag("enabled", "启用资料", true)}}
}

// payTypeDeviceField is the 支持设备 select from epay_pro's pay_type dialog:
// value 0 = PC+Mobile (the default), 1 = PC, 2 = Mobile. The original pairs
// 调用值 (type) + 支持设备 (device) as the uniqueness key; GOEPAY keeps a fixed
// type catalog, but surfaces 支持设备 so a record can declare which devices a
// method serves, matching the original column and dialog field.
func payTypeDeviceField() Field {
	f := Field{Name: "device", Label: "支持设备", Type: "select", Required: true, MaxLength: 64, Default: payTypeDeviceDefault}
	f.Options = []Option{
		{Label: "PC+Mobile", Value: "0"},
		{Label: "PC", Value: "1"},
		{Label: "Mobile", Value: "2"},
	}
	return f
}

// payTypeDeviceDefault is epay_pro's 支持设备 default (0 = PC+Mobile): a catalog
// row with no stored record, and a record saved without an explicit device,
// both read as serving both device classes.
const payTypeDeviceDefault = "0"

// originalPayType mirrors the original pre_type catalog (epay_pro app/sql
// install.sql): the調用值 (type), its 名称 (showname) and the status a fresh
// install starts with. The backoffice package sits below the server routing
// layer, so the catalog is duplicated here; keep it in sync with the server's
// originalPayTypes in internal/server/pay_type_catalog.go. A type without a
// record follows this default, exactly like payTypeCatalogOpen does inside the
// routing transaction, so the console listing and the router agree.
var originalPayTypes = []struct {
	Name, ShowName string
	Open           bool
}{
	{"alipay", "支付宝", true},
	{"wxpay", "微信支付", true},
	{"qqpay", "QQ钱包", false},
	{"bank", "银联支付", false},
	{"jdpay", "京东支付", false},
	{"paypal", "PayPal", false},
	{"douyinpay", "抖音支付", false},
}

// listPayTypes returns the 支付方式 catalog the original console shows: the 7
// original pre_type rows always present with their effective 状态, merged with
// the stored pay-types records. A type that already has a record carries that
// record's id and enabled flag (so 状态 edits PATCH it); a type with no record
// is synthesised with id 0 and its original default (so 状态 POSTs a new record
// — routing already honours this via payTypeCatalogOpen). Extra records for a
// type outside the original catalog (e.g. ecny, sandbox, or several rows for
// one type) follow after the catalog, newest first, so nothing is hidden.
//
// The catalog is a fixed, tiny set, so the whole listing is returned on one
// page like the original console; Query pagination is reported as total=len.
func (s *Service) listPayTypes(ctx context.Context, p Principal) (Page, error) {
	rows, e := s.pool.Query(ctx, "SELECT id,body->>'name',body->>'type',body->'enabled'='true'::jsonb,coalesce(body->>'device','') FROM backoffice_records WHERE resource='pay-types' ORDER BY id DESC")
	if e != nil {
		return Page{}, e
	}
	type record struct {
		id      int64
		name    string
		enabled bool
		device  string
	}
	byType := map[string][]record{}
	order := []string{}
	for rows.Next() {
		var id int64
		var name, kind, device string
		var enabled bool
		if e = rows.Scan(&id, &name, &kind, &enabled, &device); e != nil {
			rows.Close()
			return Page{}, e
		}
		if _, seen := byType[kind]; !seen {
			order = append(order, kind)
		}
		byType[kind] = append(byType[kind], record{id: id, name: name, enabled: enabled, device: device})
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return Page{}, e
	}
	isOriginal := map[string]bool{}
	for _, t := range originalPayTypes {
		isOriginal[t.Name] = true
	}
	items := []Item{}
	seen := map[int64]bool{}
	appendRecord := func(kind string, rec record) {
		seen[rec.id] = true
		device := rec.device
		if device == "" {
			device = payTypeDeviceDefault
		}
		items = append(items, Item{ID: rec.id, Fields: map[string]any{
			"name": rec.name, "type": kind, "enabled": rec.enabled, "device": device,
		}})
	}
	// The 7 original types first, in catalog order. The newest record for the
	// type carries the live 状态; without a record the original default shows
	// and id 0 marks "no record yet" for the console switch.
	for _, t := range originalPayTypes {
		recs := byType[t.Name]
		if len(recs) == 0 {
			name := t.ShowName
			items = append(items, Item{ID: 0, Fields: map[string]any{
				"name": name, "type": t.Name, "enabled": t.Open, "device": payTypeDeviceDefault,
			}})
			continue
		}
		appendRecord(t.Name, recs[0])
	}
	// Any remaining records: extra rows for an original type, then records for
	// non-catalog types (ecny/sandbox), newest first, so none are hidden.
	for _, kind := range order {
		for _, rec := range byType[kind] {
			if seen[rec.id] {
				continue
			}
			if isOriginal[kind] {
				appendRecord(kind, rec)
			}
		}
	}
	for _, kind := range order {
		if isOriginal[kind] {
			continue
		}
		for _, rec := range byType[kind] {
			appendRecord(kind, rec)
		}
	}
	return Page{Items: items, Total: int64(len(items)), Page: 1, Limit: len(items)}, nil
}
