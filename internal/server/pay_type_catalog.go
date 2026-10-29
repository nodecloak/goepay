package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// errPayTypeClosed is the original "支付方式(type)不存在": the pay type is
// closed (epay_release includes/lib/Channel.php: !$paytype || $paytype['status']==0).
var errPayTypeClosed = errors.New("pay type closed")

// originalPayTypes is the original pre_type catalog (epay_pro app/sql
// install.sql, update2.sql, update3.sql): name, showname and the status a
// fresh install starts with. ecny (数字人民币) is served by some plugins but
// has no pre_type row, so like any type without a record it starts closed.
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

func payTypeDefaultOpen(payType string) bool {
	for _, t := range originalPayTypes {
		if t.Name == payType {
			return t.Open
		}
	}
	return false
}

// payTypeCatalogOpen reads the 支付方式 records inside the routing
// transaction: the type is open while any of its records is enabled. Without
// a record it follows the original default (alipay and wxpay open — so
// installs that never used the catalog keep working — the rest closed until
// an administrator enables them, like pre_type status 0).
func payTypeCatalogOpen(ctx context.Context, tx pgx.Tx, payType string) (bool, error) {
	var records, enabled int
	if e := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE body->'enabled'='true'::jsonb) FROM backoffice_records WHERE resource='pay-types' AND body->>'type'=$1`, payType).Scan(&records, &enabled); e != nil {
		return false, e
	}
	if records == 0 {
		return payTypeDefaultOpen(payType), nil
	}
	return enabled > 0, nil
}
