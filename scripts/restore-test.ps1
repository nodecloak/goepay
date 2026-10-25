$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
$restoreSuffix = [Guid]::NewGuid().ToString('N')
$restoreDatabase = "goepay_restore_$restoreSuffix"
$dumpPath = "/tmp/goepay-restore-$restoreSuffix.dump"
if ($restoreDatabase -notmatch '^goepay_restore_[a-f0-9]{32}$') { throw 'Invalid disposable database name' }
$created = $false
try {
  # Only the application's public schema belongs to this restore rehearsal.
  # Concurrent isolated tests may create/drop unrelated random schemas.
  docker compose exec -T postgres pg_dump -U goepay -d goepay_final -n public -Fc -f $dumpPath
  if ($LASTEXITCODE) { throw 'Local backup failed' }
  docker compose exec -T postgres createdb -U goepay $restoreDatabase
  if ($LASTEXITCODE) { throw 'Disposable database creation failed' }
  $created = $true
  # The validated GUID-named database was created in this invocation only.
  # Schema-selected archives include CREATE SCHEMA public; replace its empty default.
  docker compose exec -T postgres pg_restore -U goepay -d $restoreDatabase --clean --if-exists --no-owner --no-privileges --exit-on-error $dumpPath
  if ($LASTEXITCODE) { throw 'Restore failed' }
  $journalErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc 'SELECT count(*) FROM (SELECT j.id FROM journals j LEFT JOIN ledger_entries e ON e.journal_id=j.id GROUP BY j.id HAVING count(e.id)<2 OR COALESCE(sum(e.amount),0)<>0) invalid'
  if ($LASTEXITCODE -or "$journalErrors".Trim() -ne '0') { throw 'Restored journal invariant failed' }
  $balanceErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc 'SELECT count(*) FROM accounts a WHERE a.balance<>(SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.account_id=a.id)'
  if ($LASTEXITCODE -or "$balanceErrors".Trim() -ne '0') { throw 'Restored account invariant failed' }
  $migrationCount = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc 'SELECT count(*) FROM schema_migrations'
  if ($LASTEXITCODE) { throw 'Restored migration check failed' }
  if ([int]$migrationCount.Trim() -ge 40) {
    $fundingErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM settlement_fundings f WHERE NOT EXISTS(SELECT 1 FROM settlement_funding_invariants v WHERE v.request_id=f.request_id AND v.valid IS TRUE))+(SELECT count(*) FROM accounts a WHERE a.kind='settlement_reserve' AND NOT EXISTS(SELECT 1 FROM settlement_fundings f WHERE f.account_id=a.id))"
    if ($LASTEXITCODE -or "$fundingErrors".Trim() -ne '0') { throw 'Restored dedicated settlement funding invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 42) {
    $claimErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT count(*) FROM transfer_batch_execution_claims c JOIN transfer_batch_executions e ON e.execution_no=c.execution_no JOIN transfer_batch_items i ON i.transfer_id=c.transfer_id JOIN transfer_batches b ON b.id=i.batch_id WHERE i.state<>'claimed' OR i.batch_id<>e.batch_id OR b.merchant_id<>e.merchant_id OR NOT(c.transfer_id=ANY(e.transfer_ids))"
    if ($LASTEXITCODE -or "$claimErrors".Trim() -ne '0') { throw 'Restored batch authorization subset invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 49) {
    $sharingFundingErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM channel_sharing_fundings f WHERE NOT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants v WHERE v.order_id=f.order_id AND v.valid IS TRUE))+(SELECT count(*) FROM accounts a WHERE a.kind='channel_sharing_reserve' AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.account_id=a.id))+(SELECT count(*) FROM provider_attempt_sharing d JOIN orders o ON o.id=d.order_id WHERE d.declared AND o.status='paid' AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id))"
    if ($LASTEXITCODE -or "$sharingFundingErrors".Trim() -ne '0') { throw 'Restored channel sharing funding invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 53) {
    $sharingReturnErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT count(*) FROM channel_sharing_returns r WHERE NOT EXISTS(SELECT 1 FROM channel_sharing_return_invariants v WHERE v.return_id=r.id AND v.valid IS TRUE)"
    if ($LASTEXITCODE -or "$sharingReturnErrors".Trim() -ne '0') { throw 'Restored channel sharing return invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 55) {
    $sharingFinishErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT count(*) FROM channel_sharing_finishes p WHERE NOT EXISTS(SELECT 1 FROM channel_sharing_finish_invariants v WHERE v.finish_id=p.id AND v.valid IS TRUE)"
    if ($LASTEXITCODE -or "$sharingFinishErrors".Trim() -ne '0') { throw 'Restored channel sharing finish invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 57) {
    $referralRewardErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM referral_rewards r WHERE NOT EXISTS(SELECT 1 FROM referral_reward_invariants v WHERE v.reward_id=r.id AND v.valid IS TRUE))+(SELECT count(*) FROM referral_order_terms t JOIN orders o ON o.id=t.order_id WHERE t.enabled AND o.status='paid' AND NOT EXISTS(SELECT 1 FROM referral_rewards r WHERE r.order_id=t.order_id))"
    if ($LASTEXITCODE -or "$referralRewardErrors".Trim() -ne '0') { throw 'Restored referral reward invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 58) {
    $referralClawbackErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM referral_refund_clawbacks c WHERE NOT EXISTS(SELECT 1 FROM referral_clawback_invariants v WHERE v.refund_id=c.refund_id AND v.valid IS TRUE))+(SELECT count(*) FROM accounts a WHERE a.kind='referral_clawback_reserve' AND NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.escrow_account_id=a.id))+(SELECT count(*) FROM refunds f JOIN referral_rewards r ON r.order_id=f.order_id WHERE NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.refund_id=f.id) AND NOT EXISTS(SELECT 1 FROM referral_refund_clawbacks c WHERE c.reward_id=r.id AND c.state='succeeded' AND c.refund_id<f.id))"
    if ($LASTEXITCODE -or "$referralClawbackErrors".Trim() -ne '0') { throw 'Restored referral clawback invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 59) {
    $providerRefundResultErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM provider_refund_result_evidence p WHERE NOT EXISTS(SELECT 1 FROM provider_refund_result_invariants v WHERE v.refund_id=p.refund_id AND v.valid IS TRUE))+(SELECT count(*) FROM refunds r WHERE r.result_evidence_version=2 AND r.status IN ('succeeded','failed') AND NOT EXISTS(SELECT 1 FROM provider_refund_result_evidence p WHERE p.refund_id=r.id))"
    if ($LASTEXITCODE -or "$providerRefundResultErrors".Trim() -ne '0') { throw 'Restored provider refund result invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 60) {
    foreach ($planShape in @(
      @('settlement_plans','settlement_plan_head_invariants'),
      @('settlement_plan_versions','settlement_plan_definition_invariants'),
      @('settlement_schedule_preferences','settlement_plan_preference_invariants'),
      @('settlement_plan_authorizations','settlement_plan_authorization_invariants'),
      @('settlement_plan_runs','settlement_plan_run_invariants'),
      @('settlement_plan_items','settlement_plan_item_invariants')
    )) {
      # This closed table/view list is source-owned; it accepts no caller SQL.
      $planShapeSQL = "SELECT count(*) FROM $($planShape[0]) p WHERE NOT EXISTS(SELECT 1 FROM $($planShape[1]) v WHERE v.id=p.id AND v.valid IS TRUE)"
      $planShapeErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc $planShapeSQL
      if ($LASTEXITCODE -or "$planShapeErrors".Trim() -ne '0') { throw 'Restored settlement plan structure invariant failed' }
    }
    $planSourceErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM settlement_schedule_preference_heads h WHERE NOT EXISTS(SELECT 1 FROM settlement_schedule_preferences p WHERE (p.id,p.merchant_id,p.revision)=(h.preference_id,h.merchant_id,h.revision)))+(SELECT count(*) FROM settlement_plan_budget_uses b WHERE NOT EXISTS(SELECT 1 FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id WHERE i.id=b.item_id AND i.state='created' AND (r.authorization_id,r.plan_id,r.business_date,i.gross,i.fee,i.net)=(b.authorization_id,b.plan_id,b.business_date,b.gross,b.fee,b.net)))+(SELECT count(*) FROM settlement_scheduled_intents x WHERE NOT EXISTS(SELECT 1 FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id WHERE i.id=x.item_id AND i.state='created' AND (i.merchant_id,r.business_date,i.request_id)=(x.merchant_id,x.business_date,x.request_id)))+(SELECT count(*) FROM ledger_requests r WHERE (r.settlement_plan_item_id IS NOT NULL OR EXISTS(SELECT 1 FROM ledger_request_events e WHERE e.request_id=r.id AND e.actor='system:settlement-scheduler')) AND NOT EXISTS(SELECT 1 FROM settlement_plan_items i WHERE i.id=r.settlement_plan_item_id AND i.state='created' AND i.request_id=r.id))"
    if ($LASTEXITCODE -or "$planSourceErrors".Trim() -ne '0') { throw 'Restored settlement plan original source invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 63) {
    $incomeFundingErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM order_income_holds h WHERE NOT EXISTS(SELECT 1 FROM order_income_hold_funding_evidence v WHERE v.id=h.id AND v.valid IS TRUE))+(SELECT count(*) FROM accounts a WHERE a.kind='order_income_reserve' AND NOT EXISTS(SELECT 1 FROM order_income_hold_fundings f WHERE f.account_id=a.id))"
    if ($LASTEXITCODE -or "$incomeFundingErrors".Trim() -ne '0') { throw 'Restored original order income funding invariant failed' }
    $refundFundingErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM refunds r WHERE r.funding_evidence_version=2 AND NOT EXISTS(SELECT 1 FROM refund_funding_invariants v WHERE v.refund_id=r.id AND v.valid IS TRUE))+(SELECT count(*) FROM accounts a WHERE a.kind='order_income_refund_reserve' AND NOT EXISTS(SELECT 1 FROM refund_fundings f WHERE f.reserve_account_id=a.id))"
    if ($LASTEXITCODE -or "$refundFundingErrors".Trim() -ne '0') { throw 'Restored original refund funding invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 65) {
    $legacyHistoryErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc "SELECT (SELECT count(*) FROM legacy_refund_history h WHERE NOT EXISTS(SELECT 1 FROM legacy_import_runs r WHERE r.export_id=h.export_id AND r.sha256=h.source_hash) OR NOT EXISTS(SELECT 1 FROM orders o WHERE (o.id,o.merchant_id,o.trade_no)=(h.order_id,h.merchant_id,h.trade_no)))+(SELECT count(*) FROM legacy_import_runs r WHERE r.refund_history_version=2 AND NOT EXISTS(SELECT 1 FROM legacy_refund_manifests m WHERE m.export_id=r.export_id AND (m.source,m.source_hash)=(r.source,r.sha256) AND m.refund_count=(SELECT count(*) FROM legacy_refund_history h WHERE h.export_id=r.export_id) AND m.unknown_count=(SELECT count(*) FROM legacy_refund_history h WHERE h.export_id=r.export_id AND h.legacy_status='unknown')))"
    if ($LASTEXITCODE -or "$legacyHistoryErrors".Trim() -ne '0') { throw 'Restored legacy refund history parent invariant failed' }
  }
  if ([int]$migrationCount.Trim() -ge 62) {
    $printingErrors = docker compose exec -T postgres psql -U goepay -d $restoreDatabase -tAc 'SELECT count(*) FROM printing_intents i WHERE NOT EXISTS(SELECT 1 FROM printing_intent_invariants v WHERE v.id=i.id AND v.valid IS TRUE)'
    if ($LASTEXITCODE -or "$printingErrors".Trim() -ne '0') { throw 'Restored printing intent structure invariant failed' }
    # Read the existing local container DSN into memory only. Never print or
    # persist its credentials. Only the exact GUID database created above may
    # replace its database path for this read-only HMAC/ledger verification.
    if (-not $created -or $restoreDatabase -notmatch '^goepay_restore_[a-f0-9]{32}$') { throw 'Invalid created restore target' }
    $activeRestoreDsn = docker compose exec -T api printenv DATABASE_URL
    if ($LASTEXITCODE -or -not $activeRestoreDsn) { throw 'Local API connection unavailable for restore check' }
    try { $restoreDsnBuilder = [System.UriBuilder]::new("$activeRestoreDsn".Trim()) } catch { throw 'Invalid local API connection for restore check' }
    if ($restoreDsnBuilder.Scheme -notin @('postgres','postgresql') -or $restoreDsnBuilder.Host -ne 'postgres' -or $restoreDsnBuilder.Path -ne '/goepay_final') { throw 'Restore check requires the local Compose database' }
    $restoreDsnBuilder.Path = '/' + $restoreDatabase
    $restoreReadOnlyDsn = $restoreDsnBuilder.Uri.AbsoluteUri
    docker compose exec -T -e "DATABASE_URL=$restoreReadOnlyDsn" api /app/cli reconcile
    if ($LASTEXITCODE) { throw 'Restored HMAC and ledger verification failed' }
    $activeRestoreDsn = $null
    $restoreReadOnlyDsn = $null
    $restoreDsnBuilder = $null
  }
  Write-Output "Local backup/restore passed; journal, balance and applicable funding/authorization subset violations=0; migrations=$($migrationCount.Trim())."
} finally {
  if ($created) {
    # Only this invocation's exact disposable database is ever removed.
    docker compose exec -T postgres dropdb -U goepay $restoreDatabase
  }
  docker compose exec -T postgres rm -f -- $dumpPath
}
