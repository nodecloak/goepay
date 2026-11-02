package payment

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// 商家转账（新）: the original admin/set.php 微信V3转账产品类型
// (transfer_wxpay_type) with 微信商家转账-场景ID / 报备信息类型 / 报备信息内容
// (transfer_wxpay_scene_id / _info_type / _info_content), read for every bill.
var (
	ErrMerchantTransferOff    = fmt.Errorf("%w: 微信V3转账产品类型未选择商家转账（新）", ErrState)
	ErrMerchantTransferScene  = fmt.Errorf("%w: 未配置转账场景ID", ErrState)
	ErrMerchantTransferReport = fmt.Errorf("%w: 未配置转账报备信息", ErrState)
)

// MerchantTransferSetupMessage returns the original wording of a settings gap.
func MerchantTransferSetupMessage(e error) (string, bool) {
	for _, known := range []error{ErrMerchantTransferOff, ErrMerchantTransferScene, ErrMerchantTransferReport} {
		if errors.Is(e, known) {
			return strings.TrimPrefix(known.Error(), ErrState.Error()+": "), true
		}
	}
	return "", false
}

var merchantTransferSceneID = regexp.MustCompile(`^[0-9]{1,8}$`)

type MerchantTransferScene struct{ SceneID, InfoTypes, InfoContents string }

func (s *Store) MerchantTransferScene(ctx context.Context) (MerchantTransferScene, error) {
	var out MerchantTransferScene
	if s == nil || s.pool == nil {
		return out, ErrInvalid
	}
	rows, e := s.pool.Query(ctx, "SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('transfer_wxpay_type','transfer_wxpay_scene_id','transfer_wxpay_info_type','transfer_wxpay_info_content') ORDER BY id")
	if e != nil {
		return out, e
	}
	defer rows.Close()
	kind := ""
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			return out, e
		}
		switch k {
		case "transfer_wxpay_type":
			kind = v
		case "transfer_wxpay_scene_id":
			out.SceneID = v
		case "transfer_wxpay_info_type":
			out.InfoTypes = v
		case "transfer_wxpay_info_content":
			out.InfoContents = v
		}
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	switch {
	case kind != "1":
		return MerchantTransferScene{}, ErrMerchantTransferOff
	case !merchantTransferSceneID.MatchString(out.SceneID):
		return MerchantTransferScene{}, ErrMerchantTransferScene
	case !adapter.ValidTransferReportInfos(out.InfoTypes, out.InfoContents):
		return MerchantTransferScene{}, ErrMerchantTransferReport
	}
	return out, nil
}

// MerchantTransferSwitch reads transfer_wxpay_type for provider capability
// checks, caching the answer for ttl. A read error keeps the last answer,
// which starts as off.
func (s *Store) MerchantTransferSwitch(ttl time.Duration) func() bool {
	var mu sync.Mutex
	var on bool
	var until time.Time
	return func() bool {
		if s == nil || s.pool == nil {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		if time.Now().Before(until) {
			return on
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var raw string
		e := s.pool.QueryRow(ctx, "SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='transfer_wxpay_type' ORDER BY id DESC LIMIT 1").Scan(&raw)
		switch {
		case e == nil:
			on = raw == "1"
		case errors.Is(e, pgx.ErrNoRows):
			on = false
		}
		until = time.Now().Add(ttl)
		return on
	}
}

func merchantTransferChannel(p adapter.Provider, typ string) bool {
	mt, ok := p.(adapter.MerchantTransferProduct)
	return ok && typ == "wxpay" && mt.UsesMerchantTransfer()
}

// merchantTransferRequest adds the site scene/report settings to a bill.
func (s *Store) merchantTransferRequest(ctx context.Context, r adapter.TransferRequest) (adapter.TransferRequest, error) {
	scene, e := s.MerchantTransferScene(ctx)
	if e != nil {
		return r, e
	}
	r.SceneID, r.ReportTypes, r.ReportContents = scene.SceneID, scene.InfoTypes, scene.InfoContents
	return r, nil
}

// recordTransferPackageTx keeps WeChat's package_info for a bill waiting for
// the payee. The package is fixed for the bill: the first one recorded stays.
func recordTransferPackageTx(ctx context.Context, tx pgx.Tx, id int64, pkg string) (bool, error) {
	if pkg == "" {
		return false, nil
	}
	if len(pkg) > 2048 || !utf8.ValidString(pkg) || strings.IndexFunc(pkg, unicode.IsControl) >= 0 {
		return false, ErrInvalid
	}
	var ready bool
	if e := tx.QueryRow(ctx, `SELECT to_regclass('notification_sources231') IS NOT NULL`).Scan(&ready); e != nil {
		return false, e
	}
	columns, values := "", ""
	if ready {
		columns = ",notification_transaction231"
		values = ",pg_current_xact_id()"
	}
	tag, e := tx.Exec(ctx, "INSERT INTO wechat_transfer_packages(transfer_id,package_info"+columns+") VALUES($1,$2"+values+") ON CONFLICT (transfer_id) DO NOTHING", id, pkg)
	return e == nil && tag.RowsAffected() == 1, e
}

// TransferPackage returns the recorded package_info of a transfer.
func (s *Store) TransferPackage(ctx context.Context, id int64) (string, error) {
	var pkg string
	e := s.pool.QueryRow(ctx, "SELECT package_info FROM wechat_transfer_packages WHERE transfer_id=$1", id).Scan(&pkg)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return pkg, e
}
