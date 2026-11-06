import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import LegacyRefundHistoryPanel from "../src/components/LegacyRefundHistoryPanel.vue";

it("requires an explicit admin merchant and reads historical assertions without mutation", async () => {
  const client = new ApiClient("admin");
  const request = vi.spyOn(client, "request").mockResolvedValue({
    items: [
      {
        id: "1",
        export_id: "local-export",
        trade_no: "OLD-TRADE",
        refund_no: "OLD-REFUND",
        amount: "50",
        legacy_status: "unknown",
        verified_payment_source: false,
      },
    ],
    next_cursor: "",
  });
  const panel = mount(LegacyRefundHistoryPanel, {
    props: { client, ownSession: true },
  });
  expect(request).not.toHaveBeenCalled();
  await panel.get("summary").trigger("click");
  await panel.get("form").trigger("submit");
  expect(request).not.toHaveBeenCalled();
  await panel.get("input").setValue("9001");
  await panel.get("form").trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
  expect(request.mock.calls[0][0]).toContain("merchant_id=9001");
  expect(request.mock.calls[0][1]?.method).toBeUndefined();
  expect(panel.text()).toContain("OLD-REFUND");
  expect(panel.text()).toContain("0.50");
  expect(panel.text()).toContain("原系统未明确");
  expect(panel.text()).toContain("不作为已核验的支付或退款结果");
  await panel.get("input").setValue("9002");
  expect(panel.text()).not.toContain("OLD-REFUND");
  expect(request).toHaveBeenCalledTimes(1);
});

it("merchant uses only own session scope and delegated sessions cannot read", async () => {
  const client = new ApiClient("merchant");
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ items: [], next_cursor: "" });
  const panel = mount(LegacyRefundHistoryPanel, {
    props: { client, ownSession: true },
  });
  await panel.get("form").trigger("submit");
  await flushPromises();
  expect(request.mock.calls[0][0]).not.toContain("merchant_id");
  await panel.setProps({ ownSession: false });
  expect(panel.find("form").exists()).toBe(false);
});
