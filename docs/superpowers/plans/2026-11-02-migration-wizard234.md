# Epay Migration Wizard Implementation Plan

> **For agentic workers:** Execute the parser, wizard service and UI tasks with independent ownership; review their contracts before integration. Use the existing test pool and retain actual test receipts.

**Goal:** A super-administrator can upload an Epay Pro/Release MySQL backup, inspect compatibility and opening balances, explicitly confirm a one-time atomic import into a fresh GOEPAY installation, and inspect its result.

**Architecture:** Parse SQL as data without executing it or connecting to MySQL. Reuse the validated legacy history importer for native merchants/orders/refunds/transfers and opening journals. Encrypt the complete original backup for preservation of every source field/table. Unsupported financial obligations block import; archived-only configuration, groups, credentials and plugin records are explicitly distinguished from active GOEPAY functionality. No automatic credential activation, channel activation, job recovery or domain cutover.

**Tech Stack:** Go, PostgreSQL, AES-GCM, existing durable original-super-admin session checks, Vue/TypeScript and Playwright.

**Spec:** User request 2026-11-02; user selected an administration-console wizard. Reference local Pro/Release installation schemas and current `internal/migration` contracts.

## Global Constraints

- Work in E:/GOEPAY; no production database import, real transactions, new Docker containers or modification of the current preview.
- Maximum SQL upload 64 MiB; parse only UTF-8 dumps, bound rows/statements/columns, reject incomplete or unsupported data-changing statements. Never execute uploaded SQL/PHP or fetch remote files.
- Only current original super-admin sessions and CSRF-authorized mutations. Bind preparation and execution to the same original account; recheck authorization after database lock waits.
- Original backups contain private information: encrypt at rest, never return credentials, identity fields, raw SQL or database errors in API views/logs. Keep browser drafts in component memory only.
- Require a consistent source backup and explicit acknowledgement of archived-only records. Active frozen orders, nonzero unmapped deposits and unresolved payouts block import.
- Existing business data in the target blocks migration. Opening balances are booked once; historical paid orders produce no notifications, refunds, transfers or payment calls.
- Unknown network outcomes require a status read, never an automatic repeat mutation. Completion status and imported data commit together; failures roll back business data.

## Task 1: MySQL dump reader and mapping

**Files:** `internal/migration/dump234.go`, `dump234_test.go`, `dump_reader234.go` and synthetic fixtures in `internal/migration/testdata`.

**Interface:** `ParseDump234(raw []byte, source, prefix string, at time.Time) (Dump234, error)`. `Dump234` exposes `Source`, `Prefix`, `SHA256`, `Snapshot []byte`, `Tables []DumpTable234`, `Issues []Issue`, `Warnings []Issue`. `DumpTable234` has JSON `name`, `rows`, `mode` (`native` or `archive`). Snapshot is the existing validated Plan JSON. Preserve the complete raw input separately, never in public views.

- [x] Write failing tests for both table prefixes, quoted separators, escapes, NULL/numbers, column-list and CREATE-order inserts; malformed/truncated data, unsupported DML, duplicate columns, unknown statuses, missing references and oversized input.
- [x] Map user `uid/money/addtime`, order `uid/type/money/addtime/endtime/status`, payment type ID via the source type table. Block unsupported financial states instead of inventing a balance or payment proof. Keep all other tables explicitly archive-only.
- [x] Run the parser suite without PG/network and retain red/green logs. Reference sources remain local and ignored; tests use synthetic SQL.

## Task 2: Durable, encrypted wizard and HTTP

**Files:** `internal/migration/wizard234.go`, `wizard234_test.go`, `db/migrations/000193_migration_wizard234.up.sql`, `internal/server/migration_wizard234.go`, its tests, and the console dispatcher.

**API:** `/console/v1/admin/migration/epay` GET capabilities/target readiness; `/prepare` POST multipart `source`, `prefix`, `backup`; `/{id}` GET redacted status; `/{id}/apply` POST JSON `{expected_sha256,source_frozen,archive_acknowledged,confirmation}`. Confirmation literal is `MIGRATE`. IDs are 32 lowercase hex characters. Views contain `id,status,sha256,source,prefix,table_counts,merchant_count,order_count,refund_count,transfer_count,available_total,reserved_total,issues,warnings,target_ready,progress,report`; no source rows or credentials.

- [x] Write actual PG tests proving own-super authorization, encrypted storage, pristine-target rejection, CAS/one-use claim, rollback, replay/status recovery and no jobs/provider calls.
- [x] Keep preparations in a new migration table. Store encrypted raw SQL, source digest, encrypted native plan and protected owner metadata. Bound staging expiry and global upload concurrency.
- [x] Extend the existing importer with a transaction hook without changing ordinary CLI semantics. Acquire the importer global lock; lock business tables before the empty-target check; atomically import and mark completion. Revalidate the current original super session inside that transaction.
- [x] Add safe, redacted HTTP errors and upload limits. GET must never import or retry. Apply never automatically enables merchants/channels or executes jobs.
- [x] Run the complete migration package plus focused HTTP/session/permissions and ledger tests in GUID-owned databases on the existing pool, with zero-connection cleanup.

## Task 3: Administration wizard and operator documentation

**Files:** `web/src/components/MigrationWizard234.vue`, `web/src/migration234.ts`, App/menu wiring, unit/browser tests, `docs/migration/WIZARD.md`.

**Interface:** Props follow current privileged panels: `client: ApiClient`, `ownSession: boolean`, `adminClass?: string`. Menu `/system/migration`, title `易支付迁移`, super admins only. Consume the exact API above, send mutations only on user actions, retain an unknown-result state and provide manual status reading.

- [x] Write tests for upload/precheck, native versus archive counts, blocking issues, nonempty target, acknowledgement/confirmation, progress/completion, duplicate clicks, uncertain responses, stale session, cancel and mobile layout.
- [x] Implement upload → preview → confirm → execution/result. Explain that source credentials/configurations/plugins remain encrypted archive-only, imported merchants stay disabled, and channels must be configured before cutover. Do not call an archive-only record an activated feature.
- [x] Run frontend typecheck/unit/build and browser workflows, including actual local API migration from synthetic SQL, duplicate apply and money/job counts. Preserve the preview.
- [x] Document source stop-writes/consistent MySQL export, size/format limits, exactly mapped data, blockers, encryption/retention, transactional rollback and activation/cutover steps.

## Task 4: Review and release

- [x] Independently review uploaded-SQL handling, original-session binding, target emptiness/concurrency, encrypted backup, receipt integrity and UI mutation semantics.
- [x] Record actual validation results and unresolved limits; do not claim all-table active conversion or seamless credential cutover.
Release execution: commit/push and deploy only the tested release, preserving production configurations. Verify new migration, authentication guards, server health and read-only wizard rendering; never submit a production import. Completion is recorded in the private deployment receipt after execution, not asserted in this pre-release source snapshot.
