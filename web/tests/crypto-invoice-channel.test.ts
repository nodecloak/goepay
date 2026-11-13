import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import CryptoFundsPage from "../src/components/CryptoFundsPage.vue";
const option = {
  id: "42",
  name: "指定 TRON 通道",
  network: "tron",
  fee_bps: 75,
  min_amount: "0.1",
  max_amount: "10",
  day_limit: "100",
};
let w: VueWrapper | undefined;
afterEach(() => {
  w?.unmount();
  w = undefined;
  vi.restoreAllMocks();
});
function setup(broken = false) {
  const c = new ApiClient("merchant");
  c.csrfToken = "original";
  const request = vi
    .spyOn(c, "request")
    .mockImplementation(async (path, options) => {
      if (path === "/crypto/config")
        return {
          configured: true,
          enabled: true,
          networks: [{ network: "tron", enabled: true, fee_bps: 100 }],
        };
      if (path.startsWith("/crypto/channels")) {
        if (broken) throw Error("offline");
        return { items: [option] };
      }
      if (options?.method === "POST")
        return { checkout_url: "https://checkout.example/original" };
      return [];
    });
  w = mount(CryptoFundsPage, {
    props: { client: c, ownSession: true, section: "invoices" },
  });
  return { c, request };
}
it("uses an authorized channel and its effective fee instead of an arbitrary network", async () => {
  const { request } = setup();
  await flushPromises();
  await w!.get('[data-open-form="invoice"]').trigger("click");
  await flushPromises();
  expect(w!.get('select[name="channel_id"]').text()).toContain(
    "指定 TRON 通道",
  );
  expect(w!.text()).toContain("0.75%");
  expect(w!.find('[data-form="invoice"] select[name="network"]').exists()).toBe(
    false,
  );
  await w!.get('input[name="out_trade_no"]').setValue("original");
  await w!.get('[data-form="invoice"] input[name="amount"]').setValue("1");
  await w!.get('[data-form="invoice"]').trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/crypto/invoices",
    expect.objectContaining({
      method: "POST",
      body: expect.objectContaining({
        channel_id: "42",
        network: "tron",
        amount: "1",
      }),
    }),
  );
});
it("does not fall back to a raw network when authorized channel lookup fails", async () => {
  const { request } = setup(true);
  await flushPromises();
  await w!.get('[data-open-form="invoice"]').trigger("click");
  await flushPromises();
  expect(w!.text()).toContain("收款通道暂时无法读取");
  expect(
    w!
      .get('[data-form="invoice"] button[type="submit"]')
      .attributes("disabled"),
  ).toBeDefined();
  await w!.get('[data-form="invoice"]').trigger("submit");
  expect(request.mock.calls.some((call) => call[1]?.method === "POST")).toBe(
    false,
  );
});
