import { it, expect } from "vitest";
import {
  settlementListFixture as fixture,
  paidSettlementListFixture as paid,
} from "./fixtures/settlement-list";
import * as api from "../src/settlement-list";
const read = (value: unknown) => {
  expect(typeof api.readSettlementListRow).toBe("function");
  return api.readSettlementListRow(value);
};
it("original settlement columns preserve the administrator and merchant layouts", () => {
  expect(typeof api.originalSettlementColumns).toBe("function");
  expect(
    api.originalSettlementColumns("admin").map((c: any) => c.label),
  ).toEqual([
    "ID",
    "商户号",
    "结算方式",
    "结算账号/姓名",
    "结算金额/实际到账",
    "创建时间",
    "完成时间",
    "状态",
  ]);
  expect(
    api.originalSettlementColumns("merchant").map((c: any) => c.label),
  ).toEqual([
    "ID",
    "结算方式",
    "结算账号",
    "结算金额/实际到账",
    "创建时间",
    "完成时间",
    "状态",
  ]);
});
it("approved reservation is pending and never actual paid money", () => {
  const r = read(fixture());
  expect(r.id).toBe("9007199254740993");
  expect(r.merchantID).toBe("9223372036854775807");
  expect(r.gross).toBe("90071992547409.93");
  expect(r.expected).toBe("90071992547409.90");
  expect(r.actual).toBe("—");
  expect(r.status).toBe("待结算");
  expect(r.completedAt).toBe("—");
  expect(r.createdAt).toBe("2026-10-25 23:30:00");
});
it("trusted original successful payout uses exact amount recipient and original completion time", () => {
  const r = read(paid());
  expect(r.actual).toBe("90071992547409.90");
  expect(r.status).toBe("已完成");
  expect(r.completedAt).toBe("2026-10-25 23:35:00");
  expect(r.account).toBe("***inal");
  expect(r.name).toBe("***");
});
it("unknown failed cancelled and inconsistent success remain separate from paid", () => {
  for (const [state, label] of [
    ["pending", "正在结算"],
    ["unknown", "正在结算"],
    ["failed", "结算失败"],
    ["cancelled", "已取消"],
  ]) {
    const f = paid(),
      p = f.payout as any;
    p.state = state;
    p.send_state = state === "cancelled" ? "cancelled" : "claimed";
    p.transfer.status = state;
    p.funding.state = state === "failed" ? "released" : "reserved";
    p.funding.reserved_amount = state === "failed" ? "0" : f.amount_cents;
    const r = read(f);
    expect(r.status).toBe(label);
    expect(r.actual).toBe("—");
    expect(r.completedAt).toBe("—");
  }
  const f = paid();
  (f.payout as any).funding.state = "reserved";
  (f.payout as any).funding.reserved_amount = f.amount_cents;
  expect(read(f).status).toBe("正在结算");
  expect(read(f).actual).toBe("—");
});
it("completion dates are validated and never borrowed from creation time", () => {
  for (const value of ["", "2026-02-30 12:00:00", "not-a-date", 123]) {
    const f = paid();
    (f.payout as any).transfer.pay_date = value;
    expect(read(f).completedAt).toBe("未记录");
  }
  const f = paid();
  (f.payout as any).transfer.pay_date = "2026-10-25T15:35:00Z";
  expect(read(f).completedAt).toBe("2026-10-25 23:35:00");
});
it("conflicting identities amounts and raw private recipient fields are rejected", () => {
  for (const change of [
    (d: any) => (d.payout.merchant_id = "1"),
    (d: any) => (d.payout.transfer.amount = "1"),
    (d: any) => (d.payout.recipient.config_id = "10"),
    (d: any) => (d.amount_cents = 9007199254740993),
    (d: any) => (d.settlement_recipient.account = "must-not-display"),
  ]) {
    const f = paid();
    change(f);
    expect(() => read(f)).toThrow();
  }
});
it("missing original settlement snapshots remain explicitly unknown", () => {
  const f = {
    ...fixture(),
    status: "pending",
    settlement: null,
    settlement_funding: null,
    settlement_recipient: null,
  };
  const r = read(f);
  expect(r.status).toBe("待结算");
  expect(r.method).toBe("历史未记录");
  expect(r.expected).toBe("未记录");
  expect(r.account).toBe("历史未记录");
});
it("full but invalid projections cannot silently fall back to the legacy table", () => {
  expect(typeof api.hasOriginalSettlementProjection).toBe("function");
  expect(api.hasOriginalSettlementProjection([])).toBe(true);
  expect(api.hasOriginalSettlementProjection([fixture()])).toBe(true);
  expect(
    api.hasOriginalSettlementProjection([{ id: "1", status: "pending" }]),
  ).toBe(false);
  const bad = { ...fixture(), amount_cents: 0 };
  expect(api.hasOriginalSettlementProjection([bad])).toBe(true);
  expect(() => read(bad)).toThrow();
});
it("marks a manually-processed settlement with the original [手动] tag", () => {
  const auto = read({ ...fixture(), auto: true });
  expect(auto.method).toBe("支付宝");
  expect(auto.auto).toBe(true);
  const manual = read({ ...fixture(), auto: false });
  expect(manual.method).toBe("支付宝 [手动]");
  expect(manual.auto).toBe(false);
  const absent = { ...fixture() } as Record<string, unknown>;
  delete absent.auto;
  expect(api.hasOriginalSettlementProjection([absent])).toBe(false);
  expect(read(absent).method).toBe("支付宝 [手动]");
});
