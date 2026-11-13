# USDT node adapters

`NewTron(Config)` and `NewEthereum(Config)` implement `Adapter` using standard
node HTTP/JSON-RPC protocols. Configuration requires an explicit HTTPS endpoint
(HTTP is accepted for loopback). Constructors do not connect. No private key,
signature generation, RPC signing method, or automatic retry exists in this
package. Broadcast requires `AllowBroadcast: true`.

`VerifyNetwork` pins the complete mainnet genesis block ID, and Ethereum also
requires chain ID 1. TRON checks both wallet and walletsolidity routes.
Only [Tether's published mainnet USDT contracts](https://tether.to/en/supported-protocols/)
are accepted. Addresses normalize to TRON Base58Check or lowercase Ethereum;
mixed-case Ethereum input must pass EIP-55. Amounts are canonical decimal
strings of positive atomic uint256 units. One USDT has 1,000,000 atomic units;
balance queries can also return `"0"`.

| Operation | TRON | Ethereum |
| --- | --- | --- |
| Finalized height | `walletsolidity/getnowblock` | `eth_getBlockByNumber("finalized", false)` |
| Canonical block identity, including empty blocks | `walletsolidity/getblockbynum` | `eth_getBlockByNumber` |
| Transfer scan | Solidified blocks plus each contract execution receipt | Filtered `eth_getLogs`, receipt membership, canonical block hash |
| Receipt | `walletsolidity/gettransactionbyid` plus `gettransactioninfobyid` and block membership | `eth_getTransactionReceipt` plus canonical block hash |
| Finalized USDT balance | `walletsolidity/triggerconstantcontract` with `balanceOf(address)` | `eth_call` with `balanceOf(address)` and `"finalized"` |
| Prepare | `wallet/triggersmartcontract` with `transfer(address,uint256)` | Pending nonce or caller's reserved nonce, `eth_gasPrice`, `eth_estimateGas`, EIP-155 legacy unsigned RLP |
| Broadcast | `wallet/broadcasttransaction` | `eth_sendRawTransaction` |

Scans reject ranges above finality, return no partial batch on an RPC or
consistency failure, and default to at most 100 blocks (configurable up to
10,000). Failed execution receipts and other token contracts produce no credit.
Matched events require exact topic widths, padded addresses, and a uint256
amount. Ethereum events must occur identically in a successful receipt, and
TRON requires receipt `SUCCESS`. Duplicate transaction/event identities are
returned once. `OccurredAt` comes from the containing block (TRON milliseconds,
Ethereum seconds), never the local scan time. The application must persist
event identities and verify independent provider observations before crediting.

Prepared TRON transactions bind the node's JSON to canonical protobuf bytes and
the original request, reject native-asset value, permissions, extra contracts,
and unsupported raw fields, and validate expiration. `UnsignedPayload` is the
transaction JSON, `SigningPayload` is `raw_data_hex`, and `TxID` is its SHA-256.
`ChainID` is the decimal string `"728126428"`. `FeeLimit` is in sun and defaults
to 100,000,000.

Prepared Ethereum transactions use EIP-155 chain ID `"1"`; `UnsignedPayload` is
unsigned RLP hex and `SigningPayload` its Keccak-256 digest, both without `0x`.
`TxID` is empty until signed. `GasPrice` is decimal wei, estimated gas receives
20% headroom, and `FeeLimit` caps the maximum gas charge in wei. The application
must reserve each sender/nonce durably before signing and persist signed bytes
before submission. The isolated signer must independently decode and authorize
all prepared fields.

`Accepted` means only that the node accepted a broadcast. A timeout, missing
receipt, or `Found: false` is unresolved; it is not evidence that a replacement
payment is safe. Continue checking the original transaction ID. Only
`Found && Finalized && Success` plus matching transfer events proves a completed
USDT transfer. Replacements, nonce management, multi-provider agreement,
treasury policy, signing, accounting, and fee funding belong to the caller.

`TransactionStatus.FeeAtomic` is the actual network fee in the native asset
(TRON sun or Ethereum wei), separately from USDT amounts. TRON reads the
explicit receipt `fee`, including `0`; Ethereum multiplies `gasUsed` by
`effectiveGasPrice` using integer arithmetic. Missing required fields produce
an error and unresolved status rather than an invented zero fee. Record failed
transactions' actual fees as well as successful ones.

Protocol references:

- [TRON construction, signing, broadcast, and solidified execution semantics](https://developers.tron.network/docs/api-signature-and-broadcast-flow)
- [TRON solidified constant execution](https://developers.tron.network/reference/triggerconstantcontract-1)
- [TRON mainnet network identity](https://github.com/tronprotocol/tips/blob/master/tip-474.md)
- [Ethereum JSON-RPC](https://ethereum.org/developers/docs/apis/json-rpc/)
- [EIP-155 transaction encoding and public test vector](https://eips.ethereum.org/EIPS/eip-155)

All package tests use local `httptest` servers; no test connects to a public
blockchain or submits a real transaction.
