import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import RuntimeChannelPolicyPanel from "../src/components/RuntimeChannelPolicyPanel.vue";
import buyerFixture from "../e2e/fixtures/buyer-charge104-http.json";
it("reuses the complete fee editor for a controlled parent ID without a duplicate catalog", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "original";
  const id = "9007199254740993";
  const spy = vi
    .spyOn(c, "request")
    .mockImplementation(async (path) => path === "/buyer-fees/parents/" + id ? { ...buyerFixture.parent, channel_id: id } : {
      channel_id: id,
      pay_type: "wxpay",
      methods: ["native"],
      configured: true,
      rate: "99.50",
      fee_rate_bps: 50,
      mode: 0,
      enabled: true,
      revision: "1",
      fingerprint: "a".repeat(64),
      can_update: true,
    });
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: c, allowed: true, channelID: id } as never,
  });
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(true);
  expect(spy.mock.calls.map(([p]) => p)).toEqual([
    "/channel-policies/" + id,
    "/buyer-fees/parents/" + id,
  ]);
  expect(w.find("table").exists()).toBe(false);
  await w.get("input#runtime-channel-rate").setValue("98.75");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.text()).toContain("手续费1.25%");
  expect(spy.mock.calls.filter(([, o]) => o?.method === "PATCH")).toHaveLength(
    0,
  );
  w.unmount();
});
