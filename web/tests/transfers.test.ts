import { describe, it, expect } from "vitest";
import {
  transferPolicyError,
  transferNotice,
  cents,
  transferReadActions,
} from "../src/transfers";
describe("exact transfer limits and bank lifecycle wording", () => {
  it("compares cents without floating-point loss and respects disabled maximum", () => {
    expect(cents("99999999999999.99")).toBe(9999999999999999n);
    expect(transferPolicyError("0.09", "0.10", "1.00")).toContain("不得低于");
    expect(transferPolicyError("1.01", "0.10", "1.00")).toContain("不得高于");
    expect(transferPolicyError("1.00", "0.10", "1.00")).toBe("");
    expect(transferPolicyError("1.00", "0.10", "0.00")).toBe("");
    expect(transferPolicyError("1e2")).toContain("请输入");
  });
  it("never treats historical success as final when bank return is pending or reversed", () => {
    for (const state of ["reversed", "reversal_pending"]) {
      const message = transferNotice(
        {
          type: "bank",
          status: state,
          ledger_status: "succeeded",
          bank_lifecycle: state,
        },
        202,
      );
      expect(message).not.toContain("通道已确认付款成功");
      expect(message).toContain("退票");
    }
    expect(
      transferNotice(
        {
          type: "bank",
          status: "succeeded",
          bank_lifecycle: "reversal_pending",
        },
        200,
      ),
    ).toContain("历史账本成功不代表");
    expect(transferNotice({ type: "bank", status: "unknown" }, 202)).toContain(
      "只能查询",
    );
  });
  it("never sends provider queries or proof applications for explicitly unsent or cancelled settlement transfers", () => {
    for (const state of ["unsent", "cancelled"]) {
      expect(
        transferReadActions({
          type: "alipay",
          status: state,
          funding_source: "settlement",
          send_state: state,
        }),
      ).toEqual({ query: false, proof: false });
      expect(
        transferReadActions({
          type: "alipay",
          status: "pending",
          settlement_send_state: state,
        }),
      ).toEqual({ query: false, proof: false });
    }
    expect(
      transferReadActions({
        type: "wxpay",
        status: "unknown",
        send_state: "claimed",
      }),
    ).toEqual({ query: true, proof: false });
    expect(
      transferReadActions({ type: "alipay", status: "succeeded" }),
    ).toEqual({ query: true, proof: true });
  });
});
