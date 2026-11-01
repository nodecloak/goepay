# Group settlement minimum implementation plan

> **For agentic workers:** Execute this focused task with the existing parent task; do not create further agents.

**Goal:** Make the Pro group `settle_minmoney` field affect new settlement quotes and applications, with yuan input, blank/`_` inheritance and explicit zero.

**Architecture:** Share a typed parser between group resource validation and the existing effective-group settlement policy reader. Keep the existing transaction, fee calculation, historical replay and threshold-transfer rules. Group form rendering uses actual channel catalog types plus stored legacy types, excluding sandbox from the visible table while preserving its stored data.

**Tech Stack:** Go, PostgreSQL, Vue, Vitest.

**Spec:** Parent assignment and `2026-11-01-production-form-parity.md`.

## Constraints

- Reuse the existing synthetic PostgreSQL container on loopback port 55101; isolate each run in its own database and drop only after zero-connection verification.
- Preserve frozen preview and existing resources. No production writes, commit, push or deployment.
- Own only group files plus the narrow groups-validation hook; do not change merchant creation, account pages or ResourcePage.

## Steps

- [x] Add failing schema/CRUD, effective quote/request, inheritance/expiry/replay, and group form/catalog tests; save red logs.
- [x] Add a shared exact-money typed parser, register and validate `settle_minmoney`, and apply it in the existing effective group settlement reader.
- [x] Put the field in the settlement section and show catalog/stored payment types while preserving hidden sandbox values.
- [x] Run targeted Go/PostgreSQL and frontend regressions, typecheck, and diff checks; save logs and report evidence. Additional scheduled-authority red/green coverage fixed the reader's allowed group-policy deviations.
