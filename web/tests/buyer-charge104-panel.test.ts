import { mount, flushPromises } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";
import Panel from "../src/components/BuyerFeeSettings104.vue";
const merchant = {
  merchant_id: "9007199254740993",
  mode: 0,
  mode_feerate: "",
  configured: false,
  revision: "0",
  fingerprint: "a".repeat(64),
  can_update: true,
  partial_available: true,
};
function setup(
  handler: (
    p: string,
    o?: Record<string, unknown>,
  ) => Promise<unknown> = async () => merchant,
  allowed = true,
) {
  const client = new ApiClient("merchant");
  client.csrfToken = "c".repeat(43);
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(handler as never);
  const w = mount(Panel, {
    props: {
      client,
      kind: "merchant",
      merchantId: merchant.merchant_id,
      allowed,
    },
  });
  return { client, request, w };
}
it("renders original charge controls and sends only the exact inspected personal settings", async () => {
  const f = setup(async (_p, o) =>
    o?.method === "PATCH"
      ? {
          ...merchant,
          mode: 1,
          configured: true,
          revision: "1",
          fingerprint: "b".repeat(64),
        }
      : merchant,
  );
  await flushPromises();
  expect(f.w.text()).toContain("手续费承担方");
  expect(f.w.text()).toContain("商家承担（余额扣费）");
  expect(f.w.text()).toContain("买家承担（订单加费）");
  await f.w.get('[name="mode"]').setValue("1");
  expect(f.w.get('[name="mode_feerate"]').attributes("placeholder")).toBe(
    "留空则全部由买家承担",
  );
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.request.mock.calls[1][0]).toBe("/account/buyer-fees");
  expect(f.request.mock.calls[1][1]?.body).toEqual({
    mode: 1,
    mode_feerate: "",
    expected_fingerprint: merchant.fingerprint,
  });
  expect(f.w.text()).toContain("保存成功");
  f.w.unmount();
});
it("retains the original partial field and rejects percentages beyond evidenced precision", async () => {
  const f = setup();
  await flushPromises();
  await f.w.get('[name="mode"]').setValue("1");
  await f.w.get('[name="mode_feerate"]').setValue("0.0000000000001");
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(1);
  expect(f.w.get('[role="alert"]').text()).toContain("最多12位");
  f.w.unmount();
});
it("saves a confirmed partial percentage as exact decimal text", async () => {
  const f = setup(async (_p, o) =>
    o?.method === "PATCH"
      ? {
          ...merchant,
          mode: 1,
          mode_feerate: "3.141592",
          configured: true,
          revision: "1",
          fingerprint: "b".repeat(64),
        }
      : merchant,
  );
  await flushPromises();
  await f.w.get('[name="mode"]').setValue("1");
  await f.w.get('[name="mode_feerate"]').setValue("3.141592");
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.request.mock.calls[1][1]?.body).toEqual({
    mode: 1,
    mode_feerate: "3.141592",
    expected_fingerprint: merchant.fingerprint,
  });
  expect(f.w.text()).toContain("保存成功");
  f.w.unmount();
});
it("does not read or save from an unavailable or delegated context", async () => {
  const f = setup(undefined, false);
  await flushPromises();
  expect(f.request).not.toHaveBeenCalled();
  expect(f.w.find("form").exists()).toBe(false);
  f.w.unmount();
});
it("rejects an unconfirmed committed result and requires a fresh inspection", async () => {
  const f = setup(async (_p, o) =>
    o?.method === "PATCH"
      ? {
          ...merchant,
          mode: 1,
          configured: true,
          revision: "1",
          merchant_id: "2",
          fingerprint: "b".repeat(64),
        }
      : merchant,
  );
  await flushPromises();
  await f.w.get('[name="mode"]').setValue("1");
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.w.text()).not.toContain("保存成功");
  expect(f.w.find("form").exists()).toBe(false);
  expect(f.w.get('[role="alert"]').text()).toContain("重新读取");
  f.w.unmount();
});
it("discards a late result after the original client or CSRF context changes", async () => {
  let resolve!: (v: unknown) => void;
  const deferred = new Promise((r) => (resolve = r));
  const f = setup(async () => deferred);
  await flushPromises();
  f.client.csrfToken = "d".repeat(43);
  resolve(merchant);
  await flushPromises();
  expect(f.w.find("form").exists()).toBe(false);
  expect(f.request).toHaveBeenCalledTimes(1);
  f.w.unmount();
});
it("requires new inspection when in-place CSRF changes after a completed read", async () => {
  const f = setup();
  await flushPromises();
  await f.w.get('[name="mode"]').setValue("1");
  f.client.csrfToken = "d".repeat(43);
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(1);
  expect(f.w.find("form").exists()).toBe(false);
  f.w.unmount();
});
