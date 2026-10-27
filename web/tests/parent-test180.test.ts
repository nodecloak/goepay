import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ParentChannelTestPayment from "../src/components/ParentChannelTestPayment.vue";
import {
  readParentPaymentTest,
  parentTestMoney,
} from "../src/parent-channel-test";
const channel = "9007199254740993",
  key = "a".repeat(32),
  trade = "2026102700000180";
const result = {
  request_id: key,
  channel_id: channel,
  merchant_id: "1",
  out_trade_no: "PT" + "b".repeat(32),
  trade_no: trade,
  name: "支付测试",
  money: "1.00",
  state: "pending",
  checkout_url:
    "/checkout/" + trade + "?expires=1793050000&token=" + "c".repeat(43),
  material_type: "qrcode",
  can_query: true,
};
it("validates exact protected test scope and accepts only the original internal checkout", () => {
  expect(parentTestMoney("1")).toBe("1.00");
  expect(readParentPaymentTest(result, channel, key)).toEqual(result);
  for (const raw of [
    { ...result, channel_id: 9007199254740993 },
    { ...result, request_id: "d".repeat(32) },
    { ...result, checkout_url: "https://evil.invalid/checkout/" + trade },
    { ...result, checkout_url: result.checkout_url.replace(trade, "OTHER") },
    { ...result, api_key: "private" },
  ])
    expect(() => readParentPaymentTest(raw, channel, key)).toThrow();
});
it("recovers an uncertain first create by original GET and never repeats the POST", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "original";
  let original = "";
  const spy = vi
    .spyOn(c, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method === "POST") {
        original = (options.body as Record<string, string>).request_id;
        throw new Error("synthetic lost response");
      }
      return { ...result, request_id: original };
    });
  const w = mount(ParentChannelTestPayment, {
    props: {
      client: c,
      allowed: true,
      channelID: channel,
      fingerprint: "f".repeat(64),
    },
  });
  await flushPromises();
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.text()).toContain("结果未知");
  expect(
    (w.get("[data-parent-test-submit]").element as HTMLButtonElement).disabled,
  ).toBe(true);
  await w.get("[data-parent-test-read]").trigger("click");
  await flushPromises();
  expect(spy.mock.calls.filter(([, o]) => o?.method === "POST")).toHaveLength(
    1,
  );
  expect(w.get("a").attributes("href")).toContain("/checkout/" + trade);
  w.unmount();
});
it("drops all payment material and pending responses when the raw CSRF context changes", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "original";
  let resolve!: (v: unknown) => void;
  vi.spyOn(c, "request").mockImplementation(
    () => new Promise((r) => (resolve = r)),
  );
  const w = mount(ParentChannelTestPayment, {
    props: {
      client: c,
      allowed: true,
      channelID: channel,
      fingerprint: "f".repeat(64),
    },
  });
  await flushPromises();
  await w.get("form").trigger("submit");
  c.csrfToken = "changed";
  resolve(result);
  await flushPromises();
  expect(w.emitted("close")).toBeTruthy();
  expect(w.find("a").exists()).toBe(false);
  w.unmount();
});
