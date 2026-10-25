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
} from "../src/pro-console-fields";
import { reportRows } from "../src/reports";
import { freshListFilters, appendListFilters } from "../src/list-filters";
describe("Pro field contracts", () => {
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
      "description",
      "amount",
      "balance_before",
      "balance_after",
      "kind",
      "account_id",
      "created_at",
      "event_key",
    ]);
    expect(columns.find((c) => c.key === "balance_before")?.label).toBe(
      "账户变更前（元）",
    );
    expect(columns.find((c) => c.key === "balance_after")?.label).toBe(
      "账户变更后（元）",
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
        amount: "123",
        status: "paid",
        channel_id: "7",
        notification_state: "none",
        refunded_amount: "0",
        api_key: "secret",
        unexpected: "raw",
      },
    ]);
    expect(cols.map((c) => c.key)).toEqual([
      "trade_no",
      "amount",
      "status",
      "notification_state",
    ]);
    expect(
      cols.some((c) => c.key === "api_trade_no" || c.key === "merchant_amount"),
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
});
