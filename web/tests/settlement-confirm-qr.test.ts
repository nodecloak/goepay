// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettlementListTable from "../src/components/SettlementListTable.vue";
import { ApiClient } from "../src/api";
import {
  paidSettlementListFixture,
  settlementListFixture,
} from "./fixtures/settlement-list";

// 确认收款 QR is fetched per row from the single-record payout GET
// (/settlement-payouts/{id} returns confirm_url), then shown as a QR dialog
// exactly like the original settle.php jumpurl QR.
describe("settlement row 确认收款 QR", () => {
  beforeEach(() => {
    // jsdom lacks the qrcode canvas path; toDataURL stays pure, but guard anyway.
    vi.restoreAllMocks();
  });

  function mountTable(area: "admin" | "merchant") {
    const client = new ApiClient(area);
    const request = vi.spyOn(client, "request").mockResolvedValue({
      request: {},
      payout: {},
      confirm_url: "/pay/wxtrans?n=11&t=1&s=abc",
    } as unknown as never);
    const w = mount(SettlementListTable, {
      props: {
        items: [paidSettlementListFixture() as Record<string, unknown>],
        area,
        checked: [],
        busy: false,
        client,
      },
    });
    return { w, client, request };
  }

  it("renders a 确认收款 button on a completed settlement row for both areas", () => {
    for (const area of ["admin", "merchant"] as const) {
      const { w } = mountTable(area);
      const confirm = w
        .findAll("button")
        .filter((b) => b.text() === "确认收款");
      expect(confirm.length).toBe(1);
      w.unmount();
    }
  });

  it("fetches the row confirm_url from the payout GET and shows the QR dialog", async () => {
    const { w, request } = mountTable("merchant");
    const button = w.findAll("button").find((b) => b.text() === "确认收款")!;
    await button.trigger("click");
    await vi.waitFor(() => {
      expect(request).toHaveBeenCalled();
      expect(w.find('[role="dialog"]').exists()).toBe(true);
    });
    expect(request).toHaveBeenCalledWith(
      "/settlement-payouts/" +
        encodeURIComponent(paidSettlementListFixture().id),
    );
    const dialog = w.find('[role="dialog"]');
    expect(dialog.exists()).toBe(true);
    expect(dialog.attributes("aria-label")).toBe("确认收款二维码");
    const img = dialog.find("img");
    expect(img.exists()).toBe(true);
    expect(img.attributes("src")).toMatch(/^data:image\/png;base64,/);
    w.unmount();
  });

  it("reports when a row has no confirm_url instead of opening an empty dialog", async () => {
    const client = new ApiClient("merchant");
    vi.spyOn(client, "request").mockResolvedValue({
      request: {},
      payout: {},
      confirm_url: "",
    } as unknown as never);
    const w = mount(SettlementListTable, {
      props: {
        items: [paidSettlementListFixture() as Record<string, unknown>],
        area: "merchant",
        checked: [],
        busy: false,
        client,
      },
    });
    await w
      .findAll("button")
      .find((b) => b.text() === "确认收款")!
      .trigger("click");
    await flushPromises();
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(w.find(".alert.error").text()).toContain("确认收款二维码");
    w.unmount();
  });

  it("does not offer 确认收款 on a non-completable (pending) settlement row", () => {
    // settlementListFixture() is approved-but-unpaid with no payout → canInspectPayout
    // is false, so no 确认收款 affordance appears. The preview seed has only such rows.
    const pending = settlementListFixture() as Record<string, unknown>;
    pending.status = "pending";
    pending.payout = null;
    const client = new ApiClient("merchant");
    const w = mount(SettlementListTable, {
      props: {
        items: [pending],
        area: "merchant",
        checked: [],
        busy: false,
        client,
      },
    });
    expect(w.findAll("button").some((b) => b.text() === "确认收款")).toBe(
      false,
    );
    w.unmount();
  });
});
