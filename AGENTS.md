# Local development and Docker resources

- The user explicitly requires reusing existing containers instead of creating a
  new set for every candidate or test retry. Do not create numbered or UUID-named
  PostgreSQL containers, networks, or volumes for routine tests.
- Keep the current preview and its database intact. Read
  `.tools/preview-current-path.txt` and `CURRENT.json` to identify it; do not assume
  an old port or deployment pointer is current.
- Reuse an explicitly identified synthetic test PostgreSQL pool. Isolate runs
  with unique databases/schemas; clean up only that run's own database/schema in
  `finally`, after checking that it has no active connections. Never use FORCE or
  terminate other sessions to make cleanup succeed.
- Windows tests run on the host against an existing loopback test PostgreSQL.
  Linux tests must reuse existing available test infrastructure or wait for the
  current full test to finish. Do not start another container merely to rerun a
  subset while a full test is running.
- Before starting a test, inspect existing running tests and terminal receipts.
  Do not duplicate a running full test for the same source and platform.
- Save logs and final results outside containers before removing completed
  temporary runners. Retain the current preview and active test databases. Remove
  only positively identified, completed temporary resources; never use a global
  `docker system prune` or delete unrelated projects.
- Changes to test infrastructure must not modify frozen source snapshots or
  reinterpret skipped, interrupted, or failed tests as passed.

# Naming

- Name packages, files and identifiers after what the code does. Do not put
  ticket, increment or phase numbers (such as `captcha203`, `kyc233`,
  `orderSummary245`) into package directories, import paths, file names or new
  identifiers; record them in commit messages and plans instead.
  `internal/naming_test.go` rejects numbered package directories and new
  numbered Go files. Older numbered files and identifiers are renamed when
  their code is next changed; remove a renamed file from
  `internal/testdata/numbered-go-files.txt`, which may only shrink.
