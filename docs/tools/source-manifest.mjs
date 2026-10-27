// Original group routing model, runtime catalog and transaction-bound choices.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
const root = fileURLToPath(new URL('../../', import.meta.url));
const files = [];
const walk = relative => {
  const full = path.join(root, relative);
  if (!fs.existsSync(full)) return;
  if (fs.lstatSync(full).isSymbolicLink()) throw new Error('Source manifest does not follow symbolic links: '+relative);
  if (fs.statSync(full).isDirectory()) {
    for (const name of fs.readdirSync(full).sort()) walk(path.join(relative, name));
  } else files.push(relative.replaceAll('\\', '/'));
};
// Deliberate allowlist: never traverse .env, .secrets, reference or SDK packages.
for (const name of ['cmd','internal','db','tests','scripts','api','web/src','web/tests','web/e2e','docs/tools','go.mod','go.sum','Dockerfile','Dockerfile.test','compose.yaml','.dockerignore','.env.example','README.md','web/.dockerignore','web/index.html','web/tsconfig.json','web/vite.config.ts','web/package.json','web/package-lock.json','web/Dockerfile','web/nginx.conf','web/playwright.config.ts','web/capabilities.json','web/supplemental-capabilities.json']) walk(name);
walk('web/scripts');
walk('docs/plans/2026-10-27-original-public-testpay203.md');
walk('docs/plans/2026-10-27-original-combined-history204.md');
walk('docs/plans/2026-10-27-original-login-captcha205.md');
walk('docs/plans/2026-10-27-original-refund-events201.md');
walk('docs/plans/2026-10-27-original-native-scan202.md');
walk('docs/plans/2026-10-27-original-return-notification200.md');
walk('docs/plans/2026-10-27-original-credentials199.md');
walk('docs/plans/2026-10-27-original-profile198.md');
walk('docs/plans/2026-10-27-verified-email197.md');
walk('docs/plans/2026-10-27-historical-proof-fixtures196.md');
walk('docs/plans/2026-10-27-original-smtp-accounts195.md');
walk('docs/plans/2026-10-27-original-mail-frequency194.md');
walk('docs/plans/2026-10-27-original-site-branding193.md');
walk('docs/plans/2026-10-26-original-group-routing.md');
walk('docs/superpowers/plans/2026-10-26-original-buyer-charge104.md');
walk('docs/plans/2026-10-26-original-group-route-semantics.md');
walk('docs/plans/2026-10-26-original-default-group.md');
walk('docs/plans/2026-10-26-original-group-settlement-fees.md');
walk('docs/plans/2026-10-26-original-settlement-filters.md');
walk('docs/plans/2026-10-26-original-settlement-filter-errors.md');
walk('docs/plans/2026-10-26-original-private-recipient-search.md');
walk('docs/plans/2026-10-26-original-merchant-manual-withdrawal.md');
walk('docs/plans/2026-10-26-original-merchant-settlement-permission.md');
walk('docs/plans/2026-10-26-original-random-roll-groups.md');
walk('docs/plans/2026-10-26-original-admin-merchant-credentials.md');
walk('docs/plans/2026-10-26-original-merchant-signature-policy.md');
walk('docs/plans/2026-10-26-full-original-ux-follow-up.md');
walk('docs/plans/2026-10-26-original-transfer-and-domain-parity.md');
walk('docs/plans/2026-10-26-original-merchant-order-actions.md');
walk('docs/superpowers/plans/2026-10-26-original-merchant-daily-statistics.md');
walk('docs/superpowers/plans/2026-10-26-original-trade-table-readability.md');
walk('docs/superpowers/plans/2026-10-26-original-province-labels.md');
walk('docs/superpowers/plans/2026-10-26-original-group-payment-limits.md');
walk('docs/superpowers/plans/2026-10-26-original-group-payment-type-rates.md');
walk('docs/superpowers/plans/2026-10-26-original-group-directory.md');
walk('docs/superpowers/plans/2026-10-26-original-group-statistics.md');
walk('docs/superpowers/plans/2026-10-26-original-group-transfer-access.md');
walk('docs/superpowers/plans/2026-10-26-disabled-group-transfer-access.md');
walk('docs/superpowers/plans/2026-10-26-original-merchant-list-columns.md');
// Source-bound specifications and local map provenance for the original
// statistics/log UX; never include the proprietary source package itself.
for(const name of ['docs/design/province-map-source-136.md','docs/superpowers/plans/2026-10-26-original-province-statistics.md','docs/superpowers/plans/2026-10-26-original-daily-channel-statistics.md','docs/superpowers/plans/2026-10-25-original-system-operation-log.md'])walk(name);
walk('docs/plans/2026-10-26-original-group-editor-layout.md');
walk('docs/plans/2026-10-26-original-merchant-payment-statistics.md');
walk('docs/plans/2026-10-26-original-merchant-related-records.md');
walk('docs/verification/local-preview-ux146-2026-10-26.md');
walk('docs/plans/2026-10-26-original-settlement-list-layout.md');
walk('docs/verification/local-preview-ux148-2026-10-26.md');
walk('docs/plans/2026-10-26-original-transfer-list-layout.md');
walk('docs/verification/local-preview-ux149-2026-10-26.md');
walk('docs/plans/2026-10-26-original-transfer-search.md');
// Native mini clients: enumerate source roots only, never generated packages or
// developer-tool local/private configuration.
for (const name of ['clients/mini/src','clients/mini/templates','clients/mini/tests','clients/mini/build.mjs','clients/mini/README.md','docs/design/mini-client-81.md','docs/superpowers/plans/2026-10-24-mini-client-81.md']) walk(name);
for (const name of ['docs/design/merchant-payment-review-82.md','docs/superpowers/plans/2026-10-24-merchant-payment-review-82.md']) walk(name);
for(const name of ['docs/design/admin-business-notices-83.md','docs/superpowers/plans/2026-10-24-admin-business-notices-83.md'])walk(name);
for(const name of ['docs/design/settlement-batches-84.md','docs/superpowers/plans/2026-10-25-settlement-batches-84.md','docs/verification/settlement-batches-84-2026-10-25.md'])walk(name);
// Reviewed incremental contracts; no runtime environment or imported secrets.
for(const name of ['docs/design/wechat-v2-sharing-79.md','docs/verification/wechat-v2-sharing-79-2026-10-24.md','docs/design/merchant-notice-80.md','docs/verification/merchant-notice-80-foundation-2026-10-24.md','docs/superpowers/plans/2026-10-24-wechat-v2-sharing-79.md','docs/superpowers/plans/2026-10-24-merchant-notice-80.md'])walk(name);
// Preserve the reviewed75 documents already included in frozen89, and carry
// the corresponding76 design/verification boundary with the new source.
for(const name of ['docs/design/preauth-remaining-release-75.md','docs/verification/preauth-remaining-release-75-2026-10-24.md','docs/design/channel-auto-stop-76.md','docs/verification/channel-auto-stop-76-2026-10-24.md','docs/design/admin-notice-transports-77.md','docs/verification/admin-notice-transports-77-2026-10-24.md','docs/design/scan-recovery-78.md','docs/verification/scan-recovery-78-2026-10-24.md','docs/design/printing-original-order-scope.md'])walk(name);
walk('docs/plans/2026-10-26-original-channel-rate-facts.md');
walk('docs/design/2026-10-26-channel-policy-contract.md');
walk('docs/plans/2026-10-26-pro-global-channel-sequence.md');
walk('docs/plans/2026-10-26-channel-and-referral-integration.md');
walk('docs/plans/2026-10-26-real-scheduled-tasks.md');
walk('docs/plans/2026-10-26-original-merchant-personal-limits.md');
walk('docs/plans/2026-10-26-original-merchant-api-switches.md');
walk('docs/plans/2026-10-26-original-announcements-and-welcome.md');
walk('docs/plans/2026-10-26-parent-channel-limits-integration.md');
walk('docs/plans/2026-10-26-merchant-api-switches-integration.md');
walk('docs/plans/2026-10-26-real-scheduled-tasks-integration.md');
walk('docs/plans/2026-10-26-child-parent-limits-integration.md');
walk('docs/plans/2026-10-26-original-merchant-payment-subchannels.md');
walk('docs/plans/2026-10-26-platform-merchant-access.md');
walk('docs/plans/2026-10-26-platform-access-and-child-recovery-integration.md');
walk('docs/plans/2026-10-26-original-order-business-batches.md');
walk('docs/plans/2026-10-26-original-system-update.md');
walk('docs/plans/2026-10-26-system-update-and-order-batch-integration.md');
walk('docs/core-payment-risk-compatibility.md');
walk('docs/core-payment-risk-integration.md');
walk('docs/plans/2026-10-26-original-core-payment-risk.md');
walk('docs/plans/2026-10-26-core-risk-and-strict-json-integration.md');
walk('docs/plans/2026-10-27-shared-execution-request-limits.md');
walk('docs/plans/2026-10-27-original-parent-channel-delivery.md');
walk('docs/plans/2026-10-27-original-parent-weixin-bindings.md');
walk('docs/superpowers/plans/2026-10-26-original-parent-channel-runtime.md');
walk('docs/plans/2026-10-27-parent-child-execution-integration.md');
walk('docs/plans/2026-10-27-original-site-settings191.md');
walk('docs/plans/2026-10-27-original-merchant-scanpay192.md');
const manifest = {
  generated_at: new Date().toISOString(),
  scope: 'Source snapshot only; not a claim of complete compatibility or a replacement for test results.',
  files: files.sort().map(name => ({path:name,sha256:createHash('sha256').update(fs.readFileSync(path.join(root,name))).digest('hex')})),
};
fs.mkdirSync(path.join(root,'docs/testing'),{recursive:true});
fs.writeFileSync(path.join(root,'docs/testing/source-manifest.json'),JSON.stringify(manifest,null,2)+'\n');
console.log(JSON.stringify({source_files:manifest.files.length,manifest:'docs/testing/source-manifest.json'}));

