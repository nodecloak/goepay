package backoffice

import (
	"context"
	"encoding/json"

	"goepay/internal/collectionstyle"
)

// A8-D2: the original platform ships 18 out-of-box 聚合收款码 styles seeded into
// pre_onecode by install.sql. GOEPAY stores styles as collection-styles
// backoffice records, and its forward-only migrations may not INSERT rows into
// existing tables (the history-preservation tests in db/ forbid inventing
// historical rows). So the original styles are seeded HERE, in code, by an
// idempotent startup call that only runs when no collection style exists yet —
// exactly the rows an administrator would otherwise recreate by hand on a fresh
// install, never touching an install that already has styles.
//
// All 18 originals are seeded. 15 use a public https background (img.alicdn.com)
// and are seeded verbatim with their original geometry. The three that shipped
// site-local background files (default / alipay / wxpay →
// static/files/qrcode*.{png,jpg}) have those images embedded in the binary and
// seeded as uploaded collection-style assets in the same transaction, so their
// url becomes /collection-style-assets/<id>. Every seeded style validates
// through collectionstyle.Parse (3-digit colours such as #F00 are normalised to
// #FF0000), so the generator and renderer accept them.

// defaultCollectionStyle is one seed row in the fields shape the collection-
// styles resource stores (and collectionstyle.Layout parses). AssetBackground,
// when set, is embedded image bytes seeded as an uploaded collection-style
// asset; its /collection-style-assets/<id> url then replaces URL.
type defaultCollectionStyle struct {
	Name, Label               string
	QRWidth, QRHeight         int
	Foreground, Background    string
	ImgWidth, ImgHeight       int
	Font, FontColor           string
	RecNameLeft               string // "" = centred (nil)
	RecNameTop, QRLeft, QRTop int
	URL                       string
	Sort, Word                int
	AssetBackground           []byte // nil for https-backed styles
}

// defaultCollectionStyles are all 18 originals (pre_onecode rows 1–18), in the
// original sort order. Colours are 6-hex (normalised from the original 3-hex
// where needed); backgrounds keep the original transparent. The default / alipay
// / wxpay rows carry embedded background bytes (AssetBackground) instead of an
// https url.
var defaultCollectionStyles = []defaultCollectionStyle{
	{Name: "default", Label: "默认", QRWidth: 380, QRHeight: 380, Foreground: "#555555", Background: "#FFFFFF", ImgWidth: 862, ImgHeight: 1132, Font: "60px '黑体'", FontColor: "#FFFFFF", RecNameLeft: "", RecNameTop: 210, QRLeft: 245, QRTop: 326, Sort: 0, Word: 2, AssetBackground: seedBackgroundDefault},
	{"dongxue", "冬雪", 364, 364, "#E3D6C6", "#FFFFFF", 900, 1200, "70px '黑体'", "#FFFFFF", "", 178, 270, 340, "https://img.alicdn.com/imgextra/i2/2027555802/O1CN0178X3hU1sjMWfmkoh5_!!2027555802.png", 1, 2, nil},
	{"pikaqiu", "皮卡丘", 316, 316, "#FBCE29", "#FFFFFF", 900, 1200, "70px '黑体'", "#FFFFFF", "", 178, 290, 367, "https://img.alicdn.com/imgextra/i3/2027555802/O1CN01ETL8eZ1sjMWf7GHlX_!!2027555802.png", 2, 2, nil},
	{"kanuobudingmao", "布叮猫", 316, 316, "#E1C6B1", "#FFFFFF", 900, 1200, "70px '黑体'", "#E1C6B1", "", 178, 290, 367, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01bFGxSs1sjMWcWW1uR_!!2027555802.png", 3, 2, nil},
	{"niannianyouyu", "年年有余", 370, 370, "#E62601", "#FFFFFF", 900, 1200, "60px '黑体'", "#FFFFFF", "", 255, 270, 320, "https://img.alicdn.com/imgextra/i1/2027555802/O1CN01T0g4kQ1sjMWf7HUZP_!!2027555802.png", 4, 2, nil},
	{"xiaohuangren", "小黄人", 520, 520, "#F7D25D", "#FFFFFF", 900, 1200, "70px '黑体'", "transparent", "", 178, 183, 98, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN019MNjbn1sjMWe1zPkK_!!2027555802.png", 5, 0, nil},
	{"qitao", "乞讨", 520, 520, "#212121", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 93, 98, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01vCBhEb1sjMWbLYdfv_!!2027555802.png", 6, 0, nil},
	{"baobei", "宝贝", 500, 500, "#A95B5E", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 200, 370, "https://img.alicdn.com/imgextra/i3/2027555802/O1CN01ABc7nR1sjMWbLWQRm_!!2027555802.png", 7, 0, nil},
	{"toushi", "投食", 400, 400, "#FDA3B8", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 408, 605, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01mO4EYP1sjMWeSZtzX_!!2027555802.png", 8, 0, nil},
	{"gongzhu", "公主", 520, 520, "#F36C79", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 185, 370, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01LDfMYs1sjMWhTHmPR_!!2027555802.png", 9, 0, nil},
	{"qiuzanzhu", "求赞助", 460, 460, "#F36C79", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 220, 425, "https://img.alicdn.com/imgextra/i2/2027555802/O1CN01EAhVdL1sjMWe1yXjJ_!!2027555802.png", 10, 0, nil},
	{"huanyingdashang", "欢迎打赏", 510, 510, "#FF0000", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 195, 625, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01Yuipi71sjMWhTI32u_!!2027555802.png", 11, 0, nil},
	{"yinlian", "银联", 330, 330, "#FFFFFF", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 190, 180, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01DU5ECy1sjMWe205L5_!!2027555802.png", 12, 0, nil},
	{"yitiji", "一体机", 380, 380, "#FB7C2F", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 260, 190, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01B4y11v1sjMWeSGuWy_!!2027555802.png", 13, 0, nil},
	{"maomi", "猫咪", 480, 480, "#FDD699", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 210, 150, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN017OCNEy1sjMWfe9Z3U_!!2027555802.png", 14, 0, nil},
	{"longmao", "龙猫", 464, 464, "#7394B6", "transparent", 900, 1200, "70px '黑体'", "transparent", "", 178, 218, 214, "https://img.alicdn.com/imgextra/i4/2027555802/O1CN01Jb4sxE1sjMWWkB2GX_!!2027555802.png", 15, 0, nil},
	{Name: "alipay", Label: "支付宝", QRWidth: 707, QRHeight: 707, Foreground: "#000000", Background: "#FFFFFF", ImgWidth: 1170, ImgHeight: 1755, Font: "58px 'Arial'", FontColor: "#3C3A3B", RecNameLeft: "577", RecNameTop: 1475, QRLeft: 231, QRTop: 655, Sort: 16, Word: 1, AssetBackground: seedBackgroundAlipay},
	{Name: "wxpay", Label: "微信支付", QRWidth: 486, QRHeight: 486, Foreground: "#000000", Background: "#FFFFFF", ImgWidth: 1118, ImgHeight: 1524, Font: "53px 'Arial'", FontColor: "#191919", RecNameLeft: "555", RecNameTop: 968, QRLeft: 315, QRTop: 382, Sort: 17, Word: 1, AssetBackground: seedBackgroundWxpay},
}

func (d defaultCollectionStyle) fields() map[string]any {
	body := map[string]any{
		"name":          d.Name,
		"label":         d.Label,
		"enabled":       true,
		"sort":          d.Sort,
		"url":           d.URL,
		"img_width":     d.ImgWidth,
		"img_height":    d.ImgHeight,
		"qr_width":      d.QRWidth,
		"qr_height":     d.QRHeight,
		"qr_left":       d.QRLeft,
		"qr_top":        d.QRTop,
		"foreground":    d.Foreground,
		"background":    d.Background,
		"font":          d.Font,
		"font_color":    d.FontColor,
		"rec_name_top":  d.RecNameTop,
		"word":          d.Word,
		"rec_name_left": nil,
	}
	if d.RecNameLeft != "" {
		body["rec_name_left"] = d.RecNameLeft
	}
	return body
}

// EnsureDefaultCollectionStyles seeds the original out-of-box collection styles
// the first time the console has none. It is idempotent and safe to call on
// every start: it takes a transaction advisory lock, inserts only when the
// collection-styles resource is empty, and leaves any install that already has
// styles untouched. Returns the number of styles inserted (0 when already
// seeded or on an install that had styles).
func (s *Service) EnsureDefaultCollectionStyles(ctx context.Context) (int, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	// Serialise concurrent starts so two instances never double-seed.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(85082026)"); e != nil {
		return 0, e
	}
	var existing int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM backoffice_records WHERE resource='collection-styles'").Scan(&existing); e != nil {
		return 0, e
	}
	if existing > 0 {
		return 0, nil
	}
	inserted := 0
	for _, style := range defaultCollectionStyles {
		if len(style.AssetBackground) > 0 {
			// Normalise the embedded background and store it as a collection
			// style asset (idempotent on its content hash), then point the
			// style at /collection-style-assets/<id>.
			asset, e := collectionstyle.NormalizeImage(style.AssetBackground)
			if e != nil {
				return inserted, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO collection_style_assets(id,mime,width,height,content) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING`, asset.ID, asset.MIME, asset.Width, asset.Height, asset.Bytes); e != nil {
				return inserted, e
			}
			style.URL = "/collection-style-assets/" + asset.ID
		}
		raw, e := json.Marshal(style.fields())
		if e != nil {
			return inserted, e
		}
		var id int64
		if e = tx.QueryRow(ctx, "INSERT INTO backoffice_records(resource,owner_id,body) VALUES('collection-styles',NULL,$1::jsonb) RETURNING id", string(raw)).Scan(&id); e != nil {
			return inserted, e
		}
		if e = audit(ctx, tx, Principal{Role: "admin", SubjectID: 0, Username: "system"}, "collection-styles", id, "seed-default", map[string]any{"name": style.Name}); e != nil {
			return inserted, e
		}
		inserted++
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return inserted, nil
}
