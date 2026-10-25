import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import Panel from "../src/components/ChannelSharingFinishPanel.vue";
import { ApiClient } from "../src/api";
const finish = {
  id: "16",
  order_id: "11",
  merchant_id: "1000",
  trade_no: "LOCAL_PF",
  external_no: "failed-original",
  out_no: "PF" + "a".repeat(30),
  state: "failed",
  source: "finish",
  gross_amount: "100",
  merchant_net: "90",
  shared_amount: "0",
  expected_unsplit: "100",
  release_amount: "90",
  funds_released: false,
};
it("explicitly previews credible failure before allowing a distinct new finish intent", async () => {
  const request = vi.fn(async (path: string) =>
    path.includes("finish-preview")
      ? {
          ...finish,
          id: "0",
          state: "eligible",
          external_no: "",
          out_no: "",
          previous_failed_id: "16",
        }
      : { items: [finish] },
  );
  const w = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      area: "admin",
      merchantId: "1000",
      enabled: true,
    },
  });
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
  await w.get('[data-action="prepare-finish-retry"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("原完结失败证据已核实");
  const no = w.get('[aria-label="完结业务请求号"]');
  expect((no.element as HTMLInputElement).value).not.toBe("failed-original");
  expect(no.attributes("readonly")).toBeUndefined();
  expect(request.mock.calls.every((c) => !c[0].endsWith("/query"))).toBe(true);
  expect(request.mock.calls).toHaveLength(2);
  w.unmount();
});
it("never exposes another finish intent for unknown acceptance", async () => {
  const request = vi.fn(async () => ({
    items: [{ ...finish, state: "unknown" }],
  }));
  const w = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("merchant"), { request }),
      area: "merchant",
      merchantId: "1000",
      enabled: true,
    },
  });
  await flushPromises();
  expect(w.text()).not.toContain("核对并创建新完结意图");
  expect(w.find('[data-action="query-finish"]').exists()).toBe(true);
  expect(request).toHaveBeenCalledTimes(1);
  w.unmount();
});
