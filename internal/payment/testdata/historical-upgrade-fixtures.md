# Historical upgrade fixtures

These seven compressed PostgreSQL backups contain synthetic local test facts.
They were produced by the unmodified 1,645-file historical148 source snapshot,
whose source manifest SHA-256 is
`cfdf6e68f579165049568914718c99fab7637cbb46126deb501279d7db6cd9ef`.
That snapshot passed its complete Linux and Windows Go suites (1,290 root tests
per platform). An additional test-only exporter ran each original fixture setup
against its actual old schema, then used `pg_dump --inserts --column-inserts
--no-owner --no-privileges`. All original source file hashes were checked again
after export. No production system, external provider or actual funds were used.
The notification fixture uses an in-process local mock HTTP server.

Each adjacent JSON file records the compressed and uncompressed SHA-256 and
length, original schema name, original test identifiers and source provenance.
`settlement_schema73` additionally records its actual authorization proof and
the original hashes of 13 financial/audit tables. The loader verifies both
checksums before restoring into a new isolated schema. It rejects psql commands
other than pg_dump restriction delimiters and rejects any schema rename that
would alter an inserted row value. It does not disable triggers, reseal proofs,
invent default groups, or write financial facts with current runtime code before
upgrading the database.

The migration tests still execute the actual migration under review, compare
original rows and proofs, check provider replay behavior and run the ledger
checks. Only tests that make new current-version writes first apply the rest of
the migrations. The original fixture grant table is retained without clearing its records.
The daily intent test uses its actual historical date. After verifying the
notification migration, the notice test acquires a current worker lease through
the real queue without changing its original notice proof.
