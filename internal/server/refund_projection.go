package server

// This is the recorded terminal decision time, not a provider-reported payment
// time or mutable row metadata. Missing historical evidence stays unknown.
const consoleRefundCompletedAt = `(CASE WHEN f.status IN ('succeeded','failed') THEN (
 SELECT e.created_at FROM provider_refund_result_evidence e
 WHERE e.refund_id=f.id AND e.state=f.status
 AND EXISTS(SELECT 1 FROM provider_refund_result_invariants v WHERE v.refund_id=e.refund_id AND v.valid IS TRUE)
) END) AS completed_at`
