import { mount, flushPromises } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../src/api";
import Panel from "../src/components/MerchantPersonalSettings.vue";

const mid = "9007199254740993";
const original = {
  merchant_id: mid,
  qq: "123456",
  pay_minmoney: "1.00",
  pay_maxmoney: "2.00",
  bounds_valid: true,
  fingerprint: "a".repeat(64),
  can_update: true,
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { resolve, promise };
}
function setup(
  handler?: (
    path: string,
    options?: Record<string, unknown>,
  ) => Promise<unknown>,
) {
  const client = new ApiClient("merchant");
  client.csrfToken = "c".repeat(43);
  const request = vi
    .spyOn(client, "request")
    .mockImplementation((handler || (async () => original)) as never);
  const w = mount(Panel, {
    props: { client, merchantId: mid, ownSession: true },
  });
  return { client, request, w };
}
it("reads only own real fields and saves exact original CAS without extra authority", async () => {
  const f = setup(async (_path, options) =>
    options?.method === "PATCH"
      ? { ...original, qq: "7654321", fingerprint: "b".repeat(64) }
      : original,
  );
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(1);
  expect(f.w.get('[name="qq"]').element).toHaveProperty("value", "123456");
  await f.w.get('[name="qq"]').setValue("7654321");
  expect(f.request).toHaveBeenCalledTimes(1);
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(2);
  expect(f.request.mock.calls[1][0]).toBe("/account/personal-settings");
  expect(f.request.mock.calls[1][1]?.body).toEqual({
    qq: "7654321",
    pay_minmoney: "1.00",
    pay_maxmoney: "2.00",
    expected_fingerprint: original.fingerprint,
  });
  expect(f.w.text()).toContain("保存成功");
  f.w.unmount();
});
it("personal cancellation restores the original draft without a write", async () => {
  const f = setup();
  await flushPromises();
  await f.w.get('[name="pay_minmoney"]').setValue("1.50");
  await f.w.get('[data-action="cancel"]').trigger("click");
  expect(f.w.get('[name="pay_minmoney"]').element).toHaveProperty(
    "value",
    "1.00",
  );
  expect(f.request).toHaveBeenCalledTimes(1);
  f.w.unmount();
});
it("a pending save blocks duplicate writes and reload; malformed committed results require explicit read", async () => {
  const d = deferred<unknown>(),
    f = setup(async (_path, o) =>
      o?.method === "PATCH" ? d.promise : original,
    );
  await flushPromises();
  await f.w.get("form").trigger("submit");
  await f.w.get("form").trigger("submit");
  expect(f.request).toHaveBeenCalledTimes(2);
  expect(
    f.w.get('[data-action="reload"]').attributes("disabled"),
  ).toBeDefined();
  d.resolve({
    ...original,
    merchant_id: "9007199254740995",
    fingerprint: "b".repeat(64),
  });
  await flushPromises();
  expect(f.w.text()).not.toContain("保存成功");
  expect(f.w.get('[type="submit"]').attributes("disabled")).toBeDefined();
  expect(f.w.find("form").exists()).toBe(false);
  await f.w.get('[type="submit"]').trigger("click");
  expect(f.request).toHaveBeenCalledTimes(2);
  await f.w.get('[data-action="reload"]').trigger("click");
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(3);
  f.w.unmount();
});
it("a 409 never silently reads a new fingerprint and retries", async () => {
  const f = setup(async (_path, o) => {
    if (o?.method === "PATCH") throw new ApiError("changed", 409, -1);
    return original;
  });
  await flushPromises();
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(2);
  expect(f.w.text()).toContain("重新读取");
  expect(f.w.get('[type="submit"]').attributes("disabled")).toBeDefined();
  f.w.unmount();
});
it("an unobserved same-client CSRF replacement rejects old reads and old draft writes", async () => {
  const d = deferred<unknown>(),
    f = setup(async () => d.promise);
  f.client.csrfToken = "d".repeat(43);
  d.resolve(original);
  await flushPromises();
  expect(f.w.find('[name="qq"]').exists()).toBe(false);
  expect(f.request).toHaveBeenCalledTimes(1);
  await f.w.get('[data-action="reload"]').trigger("click");
  await flushPromises();
  expect(f.w.get('[name="qq"]').element).toHaveProperty("value", "123456");
  f.client.csrfToken = "e".repeat(43);
  await f.w.get("form").trigger("submit");
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(2);
  expect(f.w.text()).not.toContain("保存成功");
  f.w.unmount();
});
it("a late save cannot enter a replacement own merchant context", async () => {
  const d = deferred<unknown>(),
    f = setup(async (_path, o) =>
      o?.method === "PATCH" ? d.promise : original,
    );
  await flushPromises();
  await f.w.get("form").trigger("submit");
  const client = new ApiClient("merchant");
  client.csrfToken = "z".repeat(43);
  const next = { ...original, merchant_id: "9007199254740995", qq: "888888" };
  vi.spyOn(client, "request").mockResolvedValue(next as never);
  await f.w.setProps({ client, merchantId: next.merchant_id });
  await flushPromises();
  d.resolve({ ...original, fingerprint: "b".repeat(64) });
  await flushPromises();
  expect(f.w.get('[name="qq"]').element).toHaveProperty("value", "888888");
  expect(f.w.text()).not.toContain("保存成功");
  f.w.unmount();
});
it("does not read or edit in a delegated view", async () => {
  const client = new ApiClient("merchant"),
    request = vi.spyOn(client, "request");
  const w = mount(Panel, {
    props: { client, merchantId: mid, ownSession: false },
  });
  await flushPromises();
  expect(request).not.toHaveBeenCalled();
  expect(w.find("form").exists()).toBe(false);
  w.unmount();
});
it("ignores a committed save response after an unobserved same-client CSRF replacement", async () => {
  const d = deferred<unknown>(),
    f = setup(async (_path, o) =>
      o?.method === "PATCH" ? d.promise : original,
    );
  await flushPromises();
  await f.w.get("form").trigger("submit");
  f.client.csrfToken = "new".repeat(14) + "x";
  d.resolve({ ...original, fingerprint: "b".repeat(64) });
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(2);
  expect(f.w.text()).not.toContain("保存成功");
  expect(f.w.text()).toContain("重新读取");
  expect(f.w.find("form").exists()).toBe(false);
  f.w.unmount();
});
