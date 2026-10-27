// The original account page now embeds these independently owned settings.
// Keep unrelated account scenarios supplied with complete read-only fixtures.
export function accountSettingsReadFixture(
  path: string,
  merchantID = "1000",
): unknown | undefined {
  if (path.endsWith("/account/email"))
    return {
      email: "",
      verified: false,
      email_enabled: false,
      requires: "password",
      fingerprint: "a".repeat(64),
      email_interval: 60,
    };
  if (path.endsWith("/account/api-switches"))
    return {
      merchant_id: merchantID,
      keylogin: 1,
      refund: 1,
      transfer: 0,
      configured: false,
      revision: "0",
      fingerprint: "a".repeat(64),
      can_update: true,
      transfer_granted: false,
    };
  if (path.endsWith("/account/personal-settings"))
    return {
      merchant_id: merchantID,
      qq: "",
      pay_minmoney: "",
      pay_maxmoney: "",
      bounds_valid: true,
      fingerprint: "a".repeat(64),
      can_update: true,
    };
  if (path.endsWith("/account/notice"))
    return {
      id: "0",
      merchant_id: merchantID,
      revision: "0",
      voice_order: false,
      email_masked: "l***@example.invalid",
      msgconfig: {
        order: 0,
        settle: 0,
        login: 0,
        complain: 0,
        mchrisk: 0,
        balance: 0,
        group: 0,
        domain_audit: 0,
        daily_report: 0,
        order_money: "0.00",
        balance_money: "0.00",
      },
      configured: {},
      target_masks: {},
    };
  if (path.endsWith("/notice/events")) return { items: [], next_cursor: "" };
  if (path.endsWith("/account/printing"))
    return {
      revision: "0",
      mode: 0,
      copies: 1,
      voice: false,
      device_masked: "",
      device_present: false,
    };
  return undefined;
}
