import { describe, it, expect } from "vitest";
import { adminOrderActions } from "../src/order-actions";
import {
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";

describe("original Pro order action menu", () => {
  it("requires independent financial authorization for the offline refund path", () => {
    expect(isAdminFundsAction("/orders/T123/manual-refund", "POST")).toBe(true);
    expect(
      needsFundsAuthorization(
        "/console/v1/admin/orders/T123/manual-refund",
        false,
      ),
    ).toBe(true);
  });
  it("keeps paid actions and notification separator in original order", () => {
    const actions = adminOrderActions({
      status: "paid",
      amount: "1000",
      refunded_amount: "0",
    });
    expect(actions.map((a) => a.label)).toEqual([
      "改未完成",
      "API退款",
      "手动退款",
      "冻结订单",
      "重新通知",
      "删除订单",
    ]);
    expect(actions.filter((a) => a.separatorBefore).map((a) => a.id)).toEqual([
      "notify",
    ]);
  });
  it("keeps unpaid actions distinct from changing status", () => {
    for (const status of ["created", "pending", "closed", "failed"]) {
      const actions = adminOrderActions({ status });
      expect(actions.map((a) => a.label)).toEqual([
        "改已完成",
        "手动补单",
        "删除订单",
      ]);
      expect(actions[1].separatorBefore).toBe(true);
    }
  });
  it("uses unfreeze for held income and never offers another freeze", () => {
    expect(
      adminOrderActions({ status: "paid", income_status: "held" }).map(
        (a) => a.label,
      ),
    ).toEqual(["解冻订单", "API退款", "重新通知", "删除订单"]);
  });
  it("shows completed refund actions separately and uses exact cents", () => {
    const row = {
      status: "paid",
      amount: "9007199254740993",
      refunded_amount: "9007199254740993",
    };
    expect(adminOrderActions(row).map((a) => a.label)).toEqual([
      "改未完成",
      "API退款",
      "改已完成",
      "重新通知",
      "删除订单",
    ]);
  });
  it("uses explicit backoffice marker without erasing the financial status", () => {
    const row = { status: "paid", admin_status: "pending" };
    expect(adminOrderActions(row).map((a) => a.label)).toEqual([
      "改已完成",
      "手动补单",
      "删除订单",
    ]);
    expect(row.status).toBe("paid");
  });
  it("lets archived rows be restored instead of triggering more financial actions", () => {
    expect(
      adminOrderActions({ status: "paid", admin_archived: true }).map(
        (a) => a.label,
      ),
    ).toEqual(["恢复订单"]);
  });
});
