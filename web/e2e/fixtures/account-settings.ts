// The original account page now embeds these independently owned settings.
// Keep unrelated account scenarios supplied with complete read-only fixtures.
export function accountSettingsReadFixture(
  path: string,
  merchantID = "1000",
): unknown | undefined {
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
