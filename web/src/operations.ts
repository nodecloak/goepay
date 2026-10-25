export type Operation =
  | "login-log"
  | "audit-log"
  | "runtime-log"
  | "system-log"
  | "jobs"
  | "system-info"
  | "cleanup";
export const operationPages: Record<string, Operation> = {
  "/system/log": "system-log",
  "/system/runtime-log": "runtime-log",
  "/system/cron": "jobs",
  "/system/clean": "cleanup",
  "/system/update": "system-info",
  "/app/plugin-list": "system-info",
};
export const canRetryJob = (row: Record<string, unknown>) =>
  typeof row.can_retry === "boolean"
    ? row.can_retry
    : row.status === "dead" &&
      [
        "payment.notify",
        "payment.reconcile",
        "refund.reconcile",
        "transfer.reconcile",
        "transfer.bank_reconcile",
        "authorization.release.reconcile",
      ].includes(String(row.kind));
