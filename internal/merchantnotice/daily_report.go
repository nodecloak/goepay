package merchantnotice

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

func paymentTypeName(kind string) string {
	switch kind {
	case "wxpay":
		return "微信支付"
	case "alipay":
		return "支付宝"
	case "qqpay":
		return "QQ钱包"
	case "bank":
		return "银行卡"
	default:
		return "其他"
	}
}
func (s *Store) dailyReport(ctx context.Context, tx pgx.Tx, mid int64, at time.Time, transfers TransferReportReader) (DailyReport, error) {
	from, until, _, err := DailyWindow(at, 0)
	if err != nil {
		return DailyReport{}, err
	}
	report := DailyReport{Date: from.Format("2006-01-02"), TypeMoney: []AmountLine{}, TransferTypes: []AmountLine{}}
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='paid' AND paid_at<=$4),COALESCE(sum(amount) FILTER(WHERE status='paid' AND paid_at<=$4),0),COALESCE(sum(merchant_amount) FILTER(WHERE status='paid' AND paid_at<=$4),0) FROM orders WHERE merchant_id=$1 AND created_at>=$2 AND created_at<$3`, mid, from, until, at).Scan(&report.OrderCount, &report.SuccessCount, &report.Money, &report.MerchantAmount)
	if err != nil {
		return report, err
	}
	rows, err := tx.Query(ctx, `SELECT type,sum(amount) FROM orders WHERE merchant_id=$1 AND created_at>=$2 AND created_at<$3 AND status='paid' AND paid_at<=$4 GROUP BY type ORDER BY type LIMIT 65`, mid, from, until, at)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var amount int64
		if err = rows.Scan(&kind, &amount); err != nil {
			return report, err
		}
		report.TypeMoney = append(report.TypeMoney, AmountLine{Name: paymentTypeName(kind), Amount: amount})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return report, err
	}
	report.HasTransfer, err = noticeTransferEnabled(ctx, tx, mid, at)
	if err != nil {
		return report, err
	}
	if report.HasTransfer {
		if transfers == nil {
			return report, ErrState
		}
		report.TransferTypes, err = transfers.NoticeTransferLines(ctx, tx, mid, from, until)
		if err != nil {
			return report, err
		}
		for _, line := range report.TransferTypes {
			if line.Amount < 0 || line.Amount > math.MaxInt64-report.TransferTotal {
				return report, ErrState
			}
			report.TransferTotal += line.Amount
		}
	}
	return report, nil
}

func noticeTransferEnabled(ctx context.Context, tx pgx.Tx, mid int64, at time.Time) (bool, error) {
	var gid int64
	var unresolved bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT group_id FROM memberships WHERE merchant_id=m.id AND expires_at>$2),m.base_group_id),m.group_baseline_unresolved FROM merchants m WHERE m.id=$1`, mid, at).Scan(&gid, &unresolved); err != nil {
		return false, err
	}
	if unresolved {
		return false, ErrState
	}
	if gid > 0 {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT body->'user_transfer' FROM backoffice_records WHERE resource='groups' AND id=$1`, gid).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrConflict
		}
		if err != nil {
			return false, err
		}
		if len(raw) > 0 && string(raw) != "null" && string(raw) != `""` {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				value = string(raw)
			}
			if value != "0" && value != "1" {
				return false, ErrState
			}
			return value == "1", nil
		}
	}
	rows, err := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key' IN('user_transfer','transfer_enabled') ORDER BY id`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	values := map[string]bool{}
	for rows.Next() {
		var key, value string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return false, err
		}
		if _, exists := values[key]; exists {
			return false, ErrConflict
		}
		if json.Unmarshal(raw, &value) != nil {
			return false, ErrState
		}
		if key == "user_transfer" {
			if value != "0" && value != "1" {
				return false, ErrState
			}
			values[key] = value == "1"
		} else {
			if value != "true" && value != "false" {
				return false, ErrState
			}
			values[key] = value == "true"
		}
	}
	if err = rows.Err(); err != nil {
		return false, err
	}
	if value, ok := values["user_transfer"]; ok {
		return value, nil
	}
	return values["transfer_enabled"], nil
}
