# Isolated USDT signer

Build `go build ./cmd/crypto-signer` and run `crypto-signer --config /signer-only/signer.json` under a separate OS identity. The API only receives its own mTLS client certificate and the signer URL; it does not receive these treasury files or this configuration. No broadcast occurs in the signer.

Provision wallets on the isolated signer host with `go run ./cmd/crypto-keygen --dir /signer-only/keys --key-id deposit-wallet`. Create the directory beforehand with owner-only permissions (0700 on Unix; on Windows restrict its NTFS ACL to the signer user, SYSTEM and Administrators). The tool rejects broader permissions, symlink directory components and existing filenames. It generates a random secp256k1 key with the mature secp256k1 library, exclusively writes and flushes `deposit-wallet.key` with 0600 permissions, and emits only `key_id`, `ethereum_address` and `tron_address` as JSON. Import the public address into the API's address pool and add the file handle to signer configuration. Keep backups under the same signer-only protection. The API host must never receive this key file.

```json
{
  "listen": "127.0.0.1:9443",
  "server_certificate": "/signer-only/tls/server.crt",
  "server_key": "/signer-only/tls/server.key",
  "client_ca": "/signer-only/tls/api-ca.crt",
  "allowed_client_sans": ["payments-api"],
  "keys": {"treasury": "/signer-only/treasury.key"},
  "state_path": "/signer-only/state/signing.jsonl",
  "rules": [{
    "chain": "ethereum",
    "network": "mainnet",
    "chain_id": "1",
    "contract": "0xdac17f958d2ee523a2206206994597c13d831ec7",
    "from": "REPLACE_WITH_TREASURY_ADDRESS",
    "key_id": "treasury",
    "destinations": ["REPLACE_WITH_APPROVED_RECIPIENT"],
    "max_amount": "1000000",
    "daily_limit": "10000000",
    "max_daily_transactions": 25,
    "max_daily_fee_atomic": "100000000000000000",
    "max_gas_limit": 100000,
    "max_gas_price": "50000000000",
    "min_nonce": 0,
    "max_nonce": 100
  }]
}
```

All amounts are positive integer atomic USDT units (six decimals); gas price is wei. The daily limit uses UTC and includes pending attempts. Set nonce bounds using an independently verified treasury nonce. The signer persistently prevents reuse of any Ethereum nonce admitted to signing. A TRON rule uses `chain: "tron"`, `chain_id: "728126428"`, contract `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t`, `max_fee_limit` in sun, `max_ttl_seconds` (at most 86400), and a signer-owned `tron_rpc_url` (HTTPS, or HTTP on loopback for local tests). The signer independently checks genesis and block references against that endpoint.

Both `max_daily_transactions` and `max_daily_fee_atomic` are required. The example permits at most 25 new signing attempts and reserves at most 100000000000000000 wei (0.1 ETH) per source wallet per UTC day. For TRON, the native daily budget is expressed in sun (for example `"1000000000"` is 1000 TRX). Each pending attempt reserves its full TRON `fee_limit` or Ethereum `gas_limit * gas_price` before any key-provider access, independently of how small the USDT amount is. Pending, signed and unknown/failed HSM attempts keep both reservations for that day; completed request replays consume nothing further. Cancellation tombstones consume neither reservation. Actual receipt fees do not refund the signer's conservative budget. The journal retains `fee_budget` across restarts; older signing records without a fee budget fail startup rather than silently resetting limits and require explicit operator reconciliation/migration.

Each treasury file contains exactly a 32-byte hexadecimal secp256k1 private key, optionally followed by a newline. Unix files must be regular and owner-only. Restrict Windows NTFS ACLs to the service identity. Use separate mTLS and treasury keys. `KeyProvider` is the integration boundary for an HSM; no digest signing endpoint is exposed.

This implementation is self custody with one signing key per wallet. It is not MPC, threshold signing or blockchain multisignature. The HSM interface is available, but no vendor HSM connector is implemented here. A compromised signer service identity can access its file-backed keys; production isolation and backup controls remain necessary.

Requests use `POST /v1/sign` with `cryptochain.PreparedTransaction`. CA verification and an exact allowed DNS/URI client SAN are required. TLS 1.3 is mandatory. Only pinned-mainnet USDT `transfer(address,uint256)` transactions are accepted. Ethereum supports EIP-155 legacy transactions; TRON supports a single TriggerSmartContract without alternate permissions, native value, or other operations. Caller digest, source, destination, amount, fees, nonce and expiration are compared with the actual transaction. The service emits a canonical signed transaction and the client independently verifies it with `VerifySigned`.

The journal is append-only and fsynced before key use and before returning the signature. Preserve the journal across restarts and backups; never reset it to clear a request. Run one service per journal. An exclusive `.lock` file prevents concurrent owners. A graceful stop removes it. After a crash, an operator must verify the old process has stopped and reconcile pending requests before removing only that journal's stale lock. Pending requests always return `ErrIndeterminate`; they never get a replacement signature. A truncated or inconsistent journal stops startup. A journal write failure disables all subsequent signing until storage is repaired and the original journal is reconciled.

`POST /v1/cancel` accepts the exact original `PreparedTransaction` over the same mTLS/SAN boundary. `Service.Cancel` fsyncs an irrevocable `cancelled` tombstone only if that request has never entered pending signing. It does not load keys, contact a node, require current allowlist approval or reject an expired request. Same-binding cancellation retries are idempotent across restarts; changed bindings conflict, and every later `Sign` for the cancelled ID returns `ErrCancelled`. Cancelled entries consume no daily amount or nonce reservation. A signed or pending request always rejects cancellation with `ErrIndeterminate`, including an HSM timeout whose signature may exist.

`CancellationReceipt` is a process-local capability with private fields, bound to SHA-256 of the complete prepared request. `VerifyCancellation` accepts only an exact binding. `Client.Cancel` constructs it only after a successful authenticated signer response with matching ID, binding and cancelled state; errors, timeouts and arbitrary JSON cannot mint a valid capability. This is not a portable signed certificate: do not serialize a receipt as proof. On a lost cancellation response, retry the same `Cancel` to recover the original proof. The executor may release an unsigned reservation or reuse its unused Ethereum nonce only after verifying that proof. A new request uses a new ID; the old ID remains permanently fenced.

The API must persist the original prepared and signed transaction before broadcasting. On timeout, retry the same request and reconcile the original transaction ID. An expired TRON transaction or an absent receipt does not authorize a new payment automatically. Request IDs bind the entire prepared request, and completed replays return the original signature even after expiry.

The example is intentionally incomplete and cannot start until treasury and destination addresses, signer-owned certificates, policy bounds, and key files are supplied. Tests use ephemeral keys, local TLS, and local node fixtures; they do not send funds or start containers.
