import { it, expect } from "vitest";
import { transferListFixture as fixture } from "./fixtures/transfer-list";
import * as api from "../src/transfer-list";
const read = (v: unknown) => {
  expect(typeof api.readTransferListRow).toBe("function");
  return api.readTransferListRow(v);
};
it("original transfer columns differ only by the administrator merchant identifier", () => {
  expect(typeof api.originalTransferColumns).toBe("function");
  expect(
    api.originalTransferColumns("admin").map((v: any) => [v.label, v.sub]),
  ).toEqual([
    ["交易号", "第三方交易号"],
    ["商户号", undefined],
    ["付款方式", "备注"],
    ["付款账号", "姓名"],
    ["付款金额", "花费金额"],
    ["手续费", undefined],
    ["提交时间", "付款时间"],
    ["状态", undefined],
  ]);
  expect(
    api.originalTransferColumns("merchant").map((v: any) => [v.label, v.sub]),
  ).toEqual([
    ["交易号", "第三方交易号"],
    ["付款方式", "备注"],
    ["付款账号", "姓名"],
    ["付款金额", "花费金额"],
    ["手续费", undefined],
    ["提交时间", "付款时间"],
    ["状态", undefined],
  ]);
});
it("transfer list preserves exact money long identifiers and original masked identity", () => {
  const r = read(fixture());
  expect(r.amount).toBe("90071992547409.93");
  expect(r.fee).toBe("0.03");
  expect(r.cost).toBe("90071992547409.96");
  expect(r.id).toBe("9007199254740993");
  expect(r.number).toBe("1000000000000000001");
  expect(r.externalNo).toBe("merchant-business-number-150");
  expect(r.method).toBe("支付宝");
  expect(r.account).toBe("***0150");
  expect(r.name).toBe("***");
  expect(r.createdAt).toBe("2026-10-25 23:30:00");
  expect(r.status).toBe("正在处理");
});
it("effective transfer states and bank reversals never inherit a historic ledger success", () => {
  for (const [status, label] of Object.entries({
    pending: "正在处理",
    unknown: "结果待核对",
    succeeded: "转账成功",
    failed: "转账失败",
    unsent: "待处理",
    cancelled: "已取消",
    reversed: "已退回",
    reversal_pending: "退回处理中",
  }))
    expect(read(fixture({ status, ledger_status: "succeeded" })).status).toBe(
      label,
    );
  expect(
    read(
      fixture({
        type: "bank",
        status: "succeeded",
        bank_lifecycle: "reversal_pending",
        ledger_status: "succeeded",
      }),
    ).status,
  ).toBe("退回处理中");
  expect(read(fixture({ type: "wxpay" })).method).toBe("微信");
});
it("legacy missing identity and malformed timestamps remain explicitly unknown", () => {
  const r = read(
    fixture({
      type: "",
      account_masked: "",
      name_masked: "",
      created_at: "not-a-date",
      biz_no: "",
    }),
  );
  expect(r.account).toBe("历史未记录");
  expect(r.method).toBe("历史未记录");
  expect(r.createdAt).toBe("未记录");
  expect(r.number).toBe("9007199254740993");
});
it("invalid complete transfer projections cannot fall back to an unverified money or private identity", () => {
  expect(typeof api.hasOriginalTransferProjection).toBe("function");
  expect(api.hasOriginalTransferProjection([fixture()])).toBe(true);
  expect(api.hasOriginalTransferProjection([{ id: "1" }])).toBe(false);
  for (const patch of [
    { id: 9007199254740993 },
    { merchant_id: "0" },
    { amount: 103 },
    { amount: "01" },
    { amount: "9223372036854775808" },
    { fee_amount: "-1" },
    { cost_amount: "1" },
    { type: "other" },
    { account_masked: "raw-private-account" },
    { name_masked: "原私人姓名" },
    { status: "unexpected" },
  ])
    expect(() => read(fixture(patch))).toThrow();
  expect(api.hasOriginalTransferProjection([fixture({ amount: 103 })])).toBe(
    true,
  );
});
