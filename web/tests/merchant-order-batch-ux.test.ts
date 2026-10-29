// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import MerchantOrderBatchControls from "../src/components/MerchantOrderBatchControls.vue";

// Rows carry the server's per-order capability flags (merchant_actions); the
// merchant batch must honour them — a row the server did not mark stays out.
function rows() {
  return [
    {
      trade_no: "T-PAID-REFUNDABLE",
      status: "paid",
      amount: "100",
      refunded_amount: "0",
      merchant_actions: { notify: true, refund: true, query: false },
    },
    {
      trade_no: "T-PENDING-QUERY",
      status: "pending",
      amount: "100",
      merchant_actions: { query: true, notify: false, refund: false },
    },
    {
      trade_no: "T-PAID-NOCAP",
      status: "paid",
      amount: "100",
      refunded_amount: "0",
      merchant_actions: {},
    },
  ];
}

// The 批量操作 menu is a dropdown (ActionDropdown); its items teleport to body.
async function openMenu(w: ReturnType<typeof mount>) {
  await w.get(".order-menu-trigger").trigger("click");
  await flushPromises();
  return Array.from(
    document.querySelectorAll('.order-action-menu [role="menuitem"]'),
  ) as HTMLElement[];
}

describe("merchant order batch controls", () => {
  it("renders a 批量操作 menu of only merchant-scoped actions with server-eligible counts", async () => {
    const client = new ApiClient("merchant");
    const w = mount(MerchantOrderBatchControls, {
      props: {
        client,
        items: rows(),
        selected: ["T-PAID-REFUNDABLE", "T-PENDING-QUERY", "T-PAID-NOCAP"],
        ownSession: true,
      },
      attachTo: document.body,
    });
    expect(w.get(".order-menu-trigger").text()).toContain("批量操作");
    const items = (await openMenu(w)).map((x) => x.textContent || "");
    // exactly Pro's merchant batch set, no admin-only op
    expect(items.some((o) => o.includes("重新通知"))).toBe(true);
    expect(items.some((o) => o.includes("手动补单"))).toBe(true);
    expect(items.some((o) => o.includes("批量退款"))).toBe(true);
    expect(
      items.some((o) => o.includes("冻结") || o.includes("改已完成")),
    ).toBe(false);
    // eligibility counts come from each row's server capability + status
    expect(items.find((o) => o.includes("重新通知"))).toContain("可执行 1 条");
    expect(items.find((o) => o.includes("手动补单"))).toContain("可执行 1 条");
    expect(items.find((o) => o.includes("批量退款"))).toContain("可执行 1 条");
    w.unmount();
  });

  it("loops 重新通知 over eligible rows via the existing per-order endpoint, never a guess", async () => {
    const client = new ApiClient("merchant");
    const calls = vi
      .spyOn(client, "requestResult")
      .mockResolvedValue({ status: 200, data: {} } as never);
    const w = mount(MerchantOrderBatchControls, {
      props: {
        client,
        items: rows(),
        selected: ["T-PAID-REFUNDABLE", "T-PAID-NOCAP"],
        ownSession: true,
      },
      attachTo: document.body,
      global: { stubs: { teleport: true } },
    });
    const items = await openMenu(w);
    await items
      .find((x) => (x.textContent || "").includes("重新通知"))!
      .click();
    await flushPromises();
    await w.get(".modal-actions .primary").trigger("click");
    await flushPromises();
    // only the capable row is notified; T-PAID-NOCAP (no cap) is skipped
    expect(calls).toHaveBeenCalledTimes(1);
    expect(calls.mock.calls[0]?.[0]).toBe("/orders/T-PAID-REFUNDABLE/notify");
    expect(calls.mock.calls[0]?.[1]?.method).toBe("POST");
    w.unmount();
  });

  it("hands batch refund to the parent as a step list (funds-authorised per order, never auto-fired)", async () => {
    const client = new ApiClient("merchant");
    const refund = vi.spyOn(client, "requestResult");
    const w = mount(MerchantOrderBatchControls, {
      props: {
        client,
        items: rows(),
        selected: ["T-PAID-REFUNDABLE", "T-PAID-NOCAP"],
        ownSession: true,
      },
      attachTo: document.body,
      global: { stubs: { teleport: true } },
    });
    const items = await openMenu(w);
    await items
      .find((x) => (x.textContent || "").includes("批量退款"))!
      .click();
    await flushPromises();
    await w.get(".modal-actions .primary").trigger("click");
    await flushPromises();
    // no refund was fired here; only the eligible trade is emitted for stepping
    expect(refund).not.toHaveBeenCalled();
    const emitted = w.emitted("refund-step");
    expect(emitted).toBeTruthy();
    expect(emitted![0]![0]).toEqual(["T-PAID-REFUNDABLE"]);
    w.unmount();
  });
});
