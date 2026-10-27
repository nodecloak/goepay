# Reusing local test infrastructure

The user requires stopping per-candidate container creation. Current project
instructions are in `AGENTS.md`. Use the existing synthetic PostgreSQL pools;
do not clone containers for each candidate, retry, or targeted test.

## Windows tests on an existing pool

```powershell
Import-Module E:/GOEPAY/scripts/TestDatabase.psm1 -Force
Invoke-GoepayTestDatabase `
  -MetadataPath E:/GOEPAY/.tools/globalaccess179-pg.json `
  -Label payment `
  -Work {
    param($database)
    go test -p 1 ./internal/payment -run '^TestSelectedCase$' -count=1
    if ($LASTEXITCODE) { throw 'Selected regression failed' }
  }
```

The metadata must identify a running, explicitly synthetic pool whose sole
published PostgreSQL binding matches its loopback port. Credentials remain local
and are not printed. The helper never invokes `docker run`, `create`, or `start`.
It assigns a UUID database to the run and sets `GOEPAY_TEST_DATABASE_URL` and
`GOEPAY_EXPECTED_TEST_DATABASE` only for that block. It checks for active sessions
and drops only its own database in `finally`, then restores the original
environment even if the test failed. It never forces a drop or closes other
sessions. A failed cleanup remains visible and leaves the database intact.

Run `scripts/test-database-safety.ps1` to verify isolation, cleanup on failure,
active-session protection, unsafe-target rejection, and no container creation.
The test uses a mocked Docker CLI and creates no containers.

The active `.tools/test196-fixed.ps1` and `.tools/test197-fixed.ps1` wrappers use
this helper. Eleven legacy launchers that previously created per-run PostgreSQL
containers now fail before their creation command, explaining the replacement.
Their original files are archived in the latest Docker cleanup report. Already
running full tests are not interrupted by this change.

## Linux checks and final acceptance

Wait for the currently running complete Linux check instead of duplicating it.
Reuse an existing idle runner and test pool where available. If the required
runtime is unavailable, report that fact rather than silently creating another
numbered environment. Do not change test scope or call a partial test complete.

## Cleanup

Archive logs and terminal receipts first. Remove a test container only after
checking its exact identity, terminal result, active network consumers, and
database connections. Remove only its exclusive temporary data volumes and empty
networks. Keep current preview services, shared development pools, baseline data,
source snapshots, and unrelated projects. Never run a global prune.
