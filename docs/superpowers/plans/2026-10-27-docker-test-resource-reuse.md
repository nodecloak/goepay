# Docker Test Resource Reuse Implementation Plan

**Goal:** Stop per-candidate container accumulation while preserving the running
preview, active full tests, and their evidence.

**Architecture:** Reuse existing explicitly configured synthetic PostgreSQL
containers, create only a UUID database per run, and remove that database in a
finally block. Apply conservative cleanup to completed historical containers.

**Tech Stack:** PowerShell 7, Docker CLI, PostgreSQL psql.

**Spec:** The user's request to continue without repeatedly creating containers;
repository instructions in `AGENTS.md`.

## Tasks

- [x] Inventory existing containers and map completed runs to terminal receipts.
- [x] Archive logs and remove completed containers and their exclusive temporary
  volumes; keep preview services and active test databases.
- [x] Add `scripts/TestDatabase.psm1` with no container creation operations, exact
  metadata validation, UUID database isolation, idle-only cleanup, and environment
  restoration even when a test fails.
- [x] First run `scripts/test-database-safety.ps1` against the missing helper,
  then implement it and verify success/failure cleanup and unsafe-target rejection
  using a mocked Docker CLI.
- [x] Update the currently used `.tools/test197-fixed.ps1` and
  `.tools/test196-fixed.ps1` wrappers to use the helper and retain test exit codes.
- [x] Perform a regression smoke test with an existing synthetic pool, confirm
  its temporary database was removed, and verify preview/API readiness.

Completed: 52 historical containers and 22 named/anonymous temporary volumes
from the primary plan were removed (the extra completed container's anonymous
volumes were also removed). Twelve containers remain: the preview, active full
tests and shared development pools. Eleven legacy per-run PostgreSQL launchers
now refuse creation. The merchant-policy regression ran through the updated 197
wrapper with exit 0, unchanged source, and zero remaining temporary databases.
Both preview and API returned HTTP 200. This task created no containers.

## Constraints

No new containers, images, networks, or volumes for this task. No changes to
application business code, frozen snapshots, runtime credentials, or test
assertions. Do not interrupt active tests. Keep failed cleanup visible and never
drop a database with an active session.
