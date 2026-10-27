package server

// Only immutable canonical journal keys and actual foreign-key journal links
// for the named order participate. No user-controlled LIKE, regex or SQL text.
// This predicate is applied after the original account balance window.
// Visibility remains the caller's inner/outer account owner predicate, so an
// actual recipient/promoter can see their own entry without reading the source
// merchant's order or accounts. An order with no visible entries reveals none.
const consoleOrderLedgerReference = `EXISTS(SELECT 1 FROM orders linked_order
 WHERE linked_order.trade_no=$2::text
 AND (
 j.event_key IN ('payment:'||linked_order.trade_no,'payment-fee:'||linked_order.trade_no)
 OR EXISTS(SELECT 1 FROM refunds linked_refund WHERE linked_refund.order_id=linked_order.id
 AND (j.event_key IN ('refund:'||linked_refund.id||':reserve','refund:'||linked_refund.id||':resolve','refund-fee:'||linked_refund.id||':reserve','refund-fee:'||linked_refund.id||':resolve')
 OR EXISTS(SELECT 1 FROM refund_fundings f LEFT JOIN refund_funding_resolutions z ON z.refund_id=f.refund_id WHERE f.refund_id=linked_refund.id
 AND j.id IN (f.hold_reserve_journal_id,f.available_reserve_journal_id,z.held_return_journal_id,z.available_return_journal_id,z.consume_journal_id))))
 OR EXISTS(SELECT 1 FROM order_income_holds h LEFT JOIN order_income_hold_fundings f ON f.hold_id=h.id LEFT JOIN order_income_hold_releases z ON z.hold_id=h.id
 WHERE h.order_id=linked_order.id AND (j.id IN (f.funding_journal_id,f.legacy_pool_journal_id,z.journal_id) OR j.event_key IN ('order-income:'||h.id||':freeze','order-income:'||h.id||':unfreeze')))
 OR EXISTS(SELECT 1 FROM order_profit_allocations a JOIN ledger_requests r ON r.id=a.request_id
 WHERE a.order_id=linked_order.id AND j.event_key IN ('request:'||r.id||':reserve','request:'||r.id||':review'))
 OR EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=linked_order.id AND j.id=f.reserve_journal_id)
 OR EXISTS(SELECT 1 FROM channel_sharing_orders s JOIN channel_sharing_order_items i ON i.sharing_id=s.id
 WHERE s.order_id=linked_order.id AND j.id=i.paid_journal_id)
 OR EXISTS(SELECT 1 FROM channel_sharing_completions c WHERE c.order_id=linked_order.id AND j.id=c.release_journal_id)
 OR EXISTS(SELECT 1 FROM channel_sharing_returns r WHERE r.order_id=linked_order.id AND j.id=r.paid_journal_id)
 OR EXISTS(SELECT 1 FROM channel_sharing_finishes f WHERE f.order_id=linked_order.id AND j.id=f.release_journal_id)
 OR EXISTS(SELECT 1 FROM referral_rewards r WHERE r.order_id=linked_order.id AND j.id=r.journal_id)
 OR EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.order_id=linked_order.id
 AND j.id IN (c.reserve_journal_id,c.contribution_journal_id,c.restore_contribution_journal_id,c.restore_reward_journal_id,c.release_remainder_journal_id))
 OR EXISTS(SELECT 1 FROM referral_daily_liabilities d WHERE d.order_id=linked_order.id AND j.id=d.funding_journal_id)
 OR EXISTS(SELECT 1 FROM referral_daily_awards a WHERE a.order_id=linked_order.id AND j.id=a.release_journal_id)
 OR EXISTS(SELECT 1 FROM referral_daily_refund_reservations r WHERE r.order_id=linked_order.id
 AND j.id IN (r.reserve_journal_id,r.contribution_journal_id,r.restore_contribution_journal_id,r.restore_pending_journal_id,r.release_remainder_journal_id))
 ))`
