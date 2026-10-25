package server

import "context"

// These joins project already stored order metadata. They do not query a
// provider or confer authority to mark an order paid or resend a notification.
// Each join is one-to-one; the latest notification is selected deterministically.
const consoleOrderReportingJoins = `
 LEFT JOIN provider_attempts console_attempt ON console_attempt.order_id=o.id
 LEFT JOIN verified_payment_receipts console_receipt ON console_receipt.order_id=o.id
 LEFT JOIN order_reporting console_report ON console_report.order_id=o.id
 LEFT JOIN payment_authorizations console_authorization ON console_authorization.order_id=o.id
 LEFT JOIN LATERAL (SELECT j.state FROM jobs j WHERE j.kind='payment.notify' AND j.payload->>'trade_no'=o.trade_no ORDER BY j.created_at DESC,j.id DESC LIMIT 1) console_notification ON true
 LEFT JOIN LATERAL (SELECT COALESCE(sum(r.amount),0)::text AS amount FROM refunds r WHERE r.order_id=o.id AND r.status='succeeded') console_refunded ON true`

const consoleOrderReportingColumns = `COALESCE(console_attempt.channel_id::text,'') AS channel_id,
 COALESCE(console_receipt.provider_trade_no,NULLIF(console_attempt.provider_trade_no,''),'') AS api_trade_no,
 COALESCE(NULLIF(console_report.buyer,''),console_authorization.buyer_id,'') AS buyer,
 COALESCE(console_report.client_ip,'') AS client_ip,
 COALESCE(console_notification.state,'none') AS notification_state,
 console_refunded.amount AS refunded_amount`

func (s *Server) orderReportingProjection(ctx context.Context, id, merchantID int64) (map[string]string, error) {
	var channel, upstream, buyer, clientIP, notification, refunded string
	err := s.Pool.QueryRow(ctx, "SELECT "+consoleOrderReportingColumns+" FROM orders o"+consoleOrderReportingJoins+" WHERE o.id=$1 AND o.merchant_id=$2", id, merchantID).Scan(&channel, &upstream, &buyer, &clientIP, &notification, &refunded)
	if err != nil {
		return nil, err
	}
	return map[string]string{"channel_id": channel, "api_trade_no": upstream, "buyer": buyer, "client_ip": clientIP, "notification_state": notification, "refunded_amount": refunded}, nil
}
