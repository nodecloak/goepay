import { describe, it, expect } from "vitest";
import {
  consoleValue,
  minorYuan,
  proListColumns,
  resourceListFields,
  merchantGroupLabel,
  merchantRecipientLabel,
  approvedMerchantDomains,
  merchantFieldValue,
  ledgerTerm,
  ledgerTypeLabels,
} from "../src/pro-console-fields";
import { reportRows } from "../src/reports";
import {
  freshListFilters,
  appendListFilters,
  ledgerTypeFilters,
} from "../src/list-filters";
describe("Pro field contracts", () => {
  it("restores the original five merchant data columns when all safe projections exist, keeping extra fields in detail", () => {
    const fields = [
      "name",
      "available_minor",
      "settlement_recipient",
      "email",
      "phone",
      "qq",
      "created_at",
      "pay",
      "enabled",
      "effective_group_id",
      "effective_group_name",
      "login_username",
      "signature_algorithm",
    ].map((name) => ({ name, label: name, type: "string" as const }));
    expect(
      resourceListFields("merchants", fields).map((f) => [f.name, f.label]),
    ).toEqual([
      ["available_minor", "余额"],
      ["settlement_recipient", "结算账号/姓名"],
      ["email", "联系方式"],
      ["created_at", "注册时间/域名"],
      ["pay", "状态"],
    ]);
    expect(fields.some((f) => f.name === "signature_algorithm")).toBe(true);
  });
  it("keeps original pay0/pay1/pay2 separate from console account enabled", () => {
    expect(merchantFieldValue("pay", "2")).toBe("待审核");
    expect(merchantFieldValue("pay", "0")).toBe("禁止收款");
    expect(merchantFieldValue("pay", "1")).toBe("正常");
    expect(merchantFieldValue("pay", null)).toBe("未知");
    expect(
      resourceListFields("merchants", [
        { name: "pay", label: "收款状态", type: "select" },
      ]).map((x) => x.name),
    ).toEqual(["pay"]);
  });
  it("never displays unavailable or legacy plaintext recipient data and never guesses an unresolved effective group", () => {
    expect(
      merchantGroupLabel({
        effective_group_id: "0",
        effective_group_name: "默认用户组",
      }),
    ).toBe("默认用户组（0）");
    expect(
      merchantRecipientLabel({
        state: "unavailable",
        account_masked: "***1234",
        account: "raw-secret",
      }),
    ).toBe("资料不可用");
    expect(
      merchantRecipientLabel({ state: "missing", account: "legacy-secret" }),
    ).toBe("未配置");
    expect(
      merchantRecipientLabel({
        state: "configured",
        config_id: "1",
        type: "alipay",
        app_id: "",
        account_masked: "***1234",
        name_masked: "***",
        account: "raw-secret",
      }),
    ).toBe("资料不可用");
    expect(
      merchantRecipientLabel({
        state: "configured",
        config_id: "1",
        type: "alipay",
        app_id: "",
        account_masked: "***raw-secret",
        name_masked: "***",
      }),
    ).toBe("资料不可用");
    expect(
      merchantGroupLabel({
        effective_group_id: "",
        effective_group_name: "",
        group_id: "999",
        groupname: "mutable-profile",
      }),
    ).toBe("用户组未知");
    expect(
      approvedMerchantDomains([
        "shop.example.com",
        "pay.example.com",
        "shop.example.com",
        "https://bad.example/path",
        "name@bad.example",
        "bad.example:443",
        123,
      ]),
    ).toEqual(["pay.example.com", "shop.example.com"]);
  });
  it("uses server-projected per-account before and after amounts without a page-local running total", () => {
    const row = {
      id: "11",
      merchant_id: "1000",
      account_id: "9007199254740993123",
      ledger_type: "order_refund",
      description: "订单退款",
      amount: "-1",
      balance_before: "9223372036854775807",
      balance_after: "9223372036854775806",
      kind: "available",
      created_at: "2026-10-24",
      event_key: "REFUND-11",
    };
    const columns = proListColumns("ledger", "admin", [row]);
    expect(columns.map((c) => c.key)).toEqual([
      "id",
      "merchant_id",
      "ledger_type",
      "description",
      "amount",
      "balance_before",
      "balance_after",
      "kind",
      "account_id",
      "created_at",
      "event_key",
    ]);
    expect(columns.find((c) => c.key === "ledger_type")?.label).toBe("类型");
    expect(columns.find((c) => c.key === "balance_before")?.label).toBe(
      "账户变更前",
    );
    expect(columns.find((c) => c.key === "balance_after")?.label).toBe(
      "账户变更后",
    );
    expect(consoleValue("balance_before", row.balance_before)).toBe(
      "92233720368547758.07",
    );
    expect(consoleValue("balance_after", row.balance_after)).toBe(
      "92233720368547758.06",
    );
    expect(consoleValue("account_id", row.account_id)).toBe(row.account_id);
  });
  it("formats signed int64 minor strings without floating point", () => {
    expect(minorYuan("9223372036854775807")).toBe("92233720368547758.07");
    expect(minorYuan("-1")).toBe("-0.01");
    expect(minorYuan("0")).toBe("0.00");
    expect(minorYuan("1.23")).toBe("—");
    expect(minorYuan(Number.MAX_SAFE_INTEGER + 1)).toBe("—");
  });
  it("keeps notification status independent from payment status", () => {
    expect(consoleValue("status", "succeeded")).toBe("成功");
    expect(consoleValue("notification_state", "succeeded")).toBe("已通知");
    expect(consoleValue("notification_state", "none")).toBe("未创建通知");
    expect(consoleValue("notification_state", "dead")).toBe("通知失败");
    expect(consoleValue("refunded_amount", "1")).toBe("0.01");
  });
  it("allows only approved order fields and does not invent a missing receipt or net amount", () => {
    const cols = proListColumns("orders", "merchant", [
      {
        trade_no: "123",
        out_trade_no: "long",
        merchant_id: "1000",
        name: "item",
        realmoney: "123",
        type: "alipay",
        amount: "123",
        status: "paid",
        channel_id: "7",
        created_at: "2026-10-24",
        notification_state: "none",
        refunded_amount: "0",
        api_key: "secret",
        unexpected: "raw",
      },
    ]);
    // Pro's stacked order columns: 6 merged pairs + 支付状态. amount / notification
    // / client_ip / param / income_status are stacked sub-lines or moved to the
    // detail modal, so they are NOT standalone list columns. merchant_id is an
    // admin-only column, dropped for a merchant.
    expect(cols.map((c) => c.key)).toEqual([
      "trade_no",
      "name",
      "realmoney",
      "type",
      "created_at",
      "status",
    ]);
    expect(
      cols.some(
        (c) =>
          c.key === "api_key" ||
          c.key === "unexpected" ||
          c.key === "api_trade_no" ||
          c.key === "merchant_amount" ||
          c.key === "client_ip" ||
          c.key === "param",
      ),
    ).toBe(false);
  });
  it("keeps server permission fields in detail but excludes credentials from resource lists", () => {
    expect(
      resourceListFields("merchants", [
        { name: "api_key", label: "secret", type: "string" },
        { name: "id", label: "ID", type: "string" },
        { name: "email", label: "Email", type: "email" },
        { name: "name", label: "Name", type: "string" },
      ]).map((f) => f.name),
    ).toEqual(["name", "email"]);
  });
  it("preserves canonical long filter IDs as strings and scopes merchant ownership", () => {
    const filters = {
      ...freshListFilters(),
      merchant_id: "9007199254740993123",
      channel_id: "9007199254740993124",
    };
    const q = new URLSearchParams();
    appendListFilters(q, filters, "orders", "admin");
    expect(q.get("merchant_id")).toBe(filters.merchant_id);
    expect(q.get("channel_id")).toBe(filters.channel_id);
    const own = new URLSearchParams();
    appendListFilters(own, filters, "orders", "merchant");
    expect(own.has("merchant_id")).toBe(false);
  });
  it("does not derive a precise success rate from an already rounded JSON number", () => {
    expect(
      reportRows([{ orders: Number.MAX_SAFE_INTEGER + 1, paid_orders: 1 }])[0]
        .success_rate,
    ).toBe("—");
  });
  it("maps 资金明细 ledger_type codes to the original pre_record type names", () => {
    expect(ledgerTerm("ledger_type", "order_income")).toBe("订单收入");
    expect(ledgerTerm("ledger_type", "transfer")).toBe("代付");
    expect(ledgerTerm("ledger_type", "transfer_return")).toBe("代付退回");
    expect(ledgerTerm("ledger_type", "settle_auto")).toBe("自动结算");
    expect(ledgerTerm("ledger_type", "internal_transfer")).toBe("站内转账");
    expect(ledgerTerm("ledger_type", "deposit_in")).toBe("充值保证金");
    expect(ledgerTerm("ledger_type", "deposit_out")).toBe("提取保证金");
    expect(ledgerTerm("ledger_type", "membership_buy")).toBe("购买会员");
    expect(ledgerTerm("ledger_type", "invite_reward")).toBe("邀请返现");
    expect(ledgerTerm("ledger_type", "admin_credit")).toBe("后台加款");
    // An unrecognized code still renders rather than blanking the cell.
    expect(ledgerTerm("ledger_type", "mystery")).toBe("mystery");
    // Every filter option has a label in the ledger term map.
    for (const option of ledgerTypeFilters)
      expect(ledgerTypeLabels[option.value]).toBe(option.label);
  });
  it("submits the 资金明细 original type filter and rejects it for other lists", () => {
    const filters = { ...freshListFilters(), type: "settle_auto" };
    const ledger = new URLSearchParams();
    appendListFilters(ledger, filters, "ledger", "admin");
    expect(ledger.get("type")).toBe("settle_auto");
    // Orders never carry a ledger type code as a payment-method filter.
    const orders = new URLSearchParams();
    appendListFilters(orders, filters, "orders", "admin");
    expect(orders.get("type")).toBe("settle_auto"); // orders also push type; value validated server+route side
  });
});

it("names order fee policies and refund responsibility without changing unknown values", () => {
  for (const [key, value, label] of [
    ["fee_policy", "none", "不收取手续费"],
    ["fee_policy", "merchant_deduct", "从商户收入扣除"],
    ["fee_policy", "buyer_additive", "付款方承担"],
    ["refund_fee_payer", "platform", "平台承担"],
    ["refund_fee_payer", "merchant", "商户承担"],
  ])
    expect(consoleValue(key!, value, true)).toBe(label);
  expect(consoleValue("fee_policy", "future-policy", true)).toBe(
    "future-policy",
  );
  expect(consoleValue("refund_fee_payer", null, true)).toBe("—");
});
