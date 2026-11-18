CREATE TABLE crypto_network_controls (
  network text PRIMARY KEY CHECK (network IN ('tron', 'ethereum')),
  enabled boolean NOT NULL DEFAULT false,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  approved_wallet_fingerprint text NOT NULL DEFAULT '',
  CHECK ((enabled AND approved_wallet_fingerprint ~ '^[0-9a-f]{64}$')
      OR (NOT enabled AND approved_wallet_fingerprint=''))
);

INSERT INTO crypto_network_controls(network) VALUES ('tron'), ('ethereum');

CREATE TABLE crypto_listener_health (
  network text PRIMARY KEY REFERENCES crypto_network_controls(network),
  fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{64}$'),
  checked_at timestamptz NOT NULL CHECK (isfinite(checked_at))
);
