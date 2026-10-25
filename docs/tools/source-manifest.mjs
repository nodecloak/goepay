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
const manifest = {
  generated_at: new Date().toISOString(),
  scope: 'Source snapshot only; not a claim of complete compatibility or a replacement for test results.',
  files: files.sort().map(name => ({path:name,sha256:createHash('sha256').update(fs.readFileSync(path.join(root,name))).digest('hex')})),
};
fs.mkdirSync(path.join(root,'docs/testing'),{recursive:true});
fs.writeFileSync(path.join(root,'docs/testing/source-manifest.json'),JSON.stringify(manifest,null,2)+'\n');
console.log(JSON.stringify({source_files:manifest.files.length,manifest:'docs/testing/source-manifest.json'}));
