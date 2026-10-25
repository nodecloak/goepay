package merchantnotice

import (
	"context"
	"goepay/internal/merchantpolicy"
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
	access, err := merchantpolicy.ReadTransferAccessAt(ctx, tx, mid, at)
	if err != nil {
		return false, err
	}
	return access.Enabled, nil
}
