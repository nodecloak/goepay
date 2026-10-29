package server

// Compute historical balances from every immutable entry in the same physical
// ledger account before applying search, dates or pagination. The inner tenant
// predicate limits the window to the authenticated merchant (or administrator
// selection); accounts belonging to other owners never enter that window.
// A single window pass also keeps full CSV exports from issuing one historical
// sum per row. Each row still represents its own account, not a combined wallet.
const consoleLedgerFrom = `(SELECT source_entry.*,
 sum(source_entry.amount) OVER (PARTITION BY source_entry.account_id ORDER BY source_entry.id ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS balance_after
 FROM ledger_entries source_entry JOIN accounts source_account ON source_account.id=source_entry.account_id
 WHERE ($1::bigint=0 OR source_account.owner_id=$1)
) e JOIN journals j ON j.id=e.journal_id JOIN accounts a ON a.id=e.account_id`

const consoleLedgerColumns = `e.id::text,a.owner_id::text AS merchant_id,a.kind,e.amount::text,j.event_key,j.description,j.created_at,
 e.account_id::text,(e.balance_after-e.amount)::text AS balance_before,e.balance_after::text AS balance_after,
 ` + ledgerTypeExpression + ` AS ledger_type`
