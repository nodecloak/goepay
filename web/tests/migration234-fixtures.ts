export function migrationView234(extra: Record<string, unknown> = {}) {
  return {
    id: "a".repeat(32),
    status: "prepared",
    sha256: "b".repeat(64),
    source: "epay_pro",
    prefix: "pre_",
    table_counts: [
      { name: "pre_user", rows: 1, mode: "native" },
      { name: "pre_order", rows: 2, mode: "native" },
      { name: "pre_config", rows: 3, mode: "archive" },
    ],
    merchant_count: 1,
    order_count: 2,
    refund_count: 0,
    transfer_count: 0,
    available_total: "12.34",
    reserved_total: "0.00",
    issues: [],
    warnings: [{ path: "pre_config", code: "archive_only" }],
    target_ready: true,
    progress: 30,
    report: null,
    ...extra,
  };
}
export function migrationCapabilities234(extra: Record<string, unknown> = {}) {
  return {
    enabled: true,
    max_upload_bytes: 67108864,
    target_ready: true,
    issues: [],
    runs: [],
    ...extra,
  };
}
export function migrationReport234() {
  return {
    refund_history_version: 2,
    valid: true,
    sha256: "c".repeat(64),
    merchant_count: 1,
    order_count: 2,
    refund_count: 0,
    transfer_count: 0,
    available_total: "12.34",
    reserved_total: "0.00",
    issues: [],
    warnings: [],
    applied: true,
    already_imported: false,
  };
}
