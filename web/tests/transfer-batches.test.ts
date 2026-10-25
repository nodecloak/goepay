import { expect, it } from "vitest";
import {
  buildBatchItems,
  batchOutcome,
  batchCanExecute,
} from "../src/transfer-batches";
const policy = {
  minimum_money: "0.01",
  maximum_money: "2.00",
  default_remark: "说明",
  default_title: "标题",
  channels: [
    { id: "1", types: ["alipay"] },
    {
      id: "2",
      types: ["bank"],
      bank_product: "alipay_bankcard",
      bank_code_required: false,
    },
    {
      id: "3",
      types: ["bank"],
      bank_product: "wxpay_v2",
      bank_code_required: true,
    },
  ],
};
const row = {
  channel_id: "1",
  type: "alipay",
  account: "wallet",
  name: "Test",
  amount: "1.00",
  remark: "",
  bank_code: "",
};
it("validates ordered batch rows with exact money, explicit bank product and defaults, without creating per-row request numbers", () => {
  expect(buildBatchItems([row], policy)).toEqual([
    {
      channel_id: "1",
      type: "alipay",
      account: "wallet",
      name: "Test",
      amount: "1.00",
      remark: "标题",
    },
  ]);
  const ali = {
    ...row,
    channel_id: "2",
    type: "bank",
    account: "6222020202020202",
    bank_code: "1002",
  };
  expect(buildBatchItems([ali], policy)[0]).not.toHaveProperty("bank_code");
  expect(() =>
    buildBatchItems([{ ...ali, channel_id: "3", bank_code: "" }], policy),
  ).toThrow("银行");
  expect(() =>
    buildBatchItems([{ ...row, amount: "2.001" }], policy),
  ).toThrow();
  expect(() => buildBatchItems([{ ...row, amount: "2.01" }], policy)).toThrow(
    "高于",
  );
  expect(() =>
    buildBatchItems(
      Array.from({ length: 101 }, () => row),
      policy,
    ),
  ).toThrow("100");
  expect(() => buildBatchItems([{ ...row, channel_id: "9" }], policy)).toThrow(
    "通道",
  );
});
it("HTTP200 is not batch success and unknown items never enable resubmission", () => {
  expect(
    batchOutcome({
      item_count: 2,
      all_succeeded: false,
      summary: { succeeded: 1, unknown: 1 },
    }),
  ).toContain("不能视为全部成功");
  expect(batchCanExecute({ summary: { unsent: 0, unknown: 2 } }, true)).toBe(
    false,
  );
  expect(batchCanExecute({ summary: { unsent: 2, unknown: 1 } }, false)).toBe(
    false,
  );
  expect(batchCanExecute({ summary: { unsent: 2, unknown: 1 } }, true)).toBe(
    true,
  );
});
