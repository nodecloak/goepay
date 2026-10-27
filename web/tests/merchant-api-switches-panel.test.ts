import { mount, flushPromises } from "@vue/test-utils";
import { expect, it, vi, beforeEach, afterEach } from "vitest";
import actualPlatform from "../e2e/globalaccess179-http-fixture.json";
beforeEach(() =>
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url !== "/console/v1/public/merchant-access")
        throw Error(`unexpected ${url}`);
      return new Response(
        JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      );
    }),
  ),
);
afterEach(() => vi.unstubAllGlobals());
import { ApiClient, ApiError } from "../src/api";
import Panel from "../src/components/MerchantAPISwitches.vue";
const mid = "9007199254740993",
  original = {
    merchant_id: mid,
    keylogin: 1,
    refund: 1,
    transfer: 0,
    configured: false,
    revision: "0",
    fingerprint: "a".repeat(64),
    can_update: true,
    transfer_granted: false,
  };
function setup(
  handler?: (p: string, o?: Record<string, unknown>) => Promise<unknown>,
) {
  const c = new ApiClient("merchant");
  c.csrfToken = "c".repeat(43);
  const request = vi
    .spyOn(c, "request")
    .mockImplementation((handler || (async () => original)) as never);
  const w = mount(Panel, {
    props: { client: c, merchantId: mid, ownSession: true },
  });
  return { c, request, w };
}
it("shows original three switches separately, confirms exact CAS and never grants admin transfer rights", async () => {
  const f = setup(async (_p, o) =>
    o?.method === "PATCH"
      ? {
          ...original,
          configured: true,
          revision: "1",
          transfer: 1,
          fingerprint: "b".repeat(64),
        }
      : original,
  );
  await flushPromises();
  expect(f.w.text()).toContain("仍需管理员");
  expect(f.w.get('[name="keylogin"]').element).toHaveProperty("checked", true);
  await f.w.get('[name="transfer"]').setValue(true);
  await f.w.get("form").trigger("submit");
  expect(f.request).toHaveBeenCalledTimes(1);
  await f.w.get('[data-action="confirm"]').trigger("click");
  await flushPromises();
  expect(f.request.mock.calls[1]?.[1]?.body).toEqual({
    keylogin: 1,
    refund: 1,
    transfer: 1,
    expected_fingerprint: original.fingerprint,
  });
  expect(f.w.text()).toContain("保存成功");
  f.w.unmount();
});
it("cancellation restores switches without writing, and delegation makes no reads", async () => {
  const f = setup();
  await flushPromises();
  await f.w.get('[name="refund"]').setValue(false);
  await f.w.get("form").trigger("submit");
  await f.w.get('[data-action="cancel-confirm"]').trigger("click");
  expect(f.request).toHaveBeenCalledTimes(1);
  expect(f.w.get('[name="refund"]').element).toHaveProperty("checked", false);
  await f.w.get('[data-action="cancel"]').trigger("click");
  expect(f.w.get('[name="refund"]').element).toHaveProperty("checked", true);
  await f.w.setProps({ ownSession: false });
  expect(f.w.find("form").exists()).toBe(false);
  expect(f.request).toHaveBeenCalledTimes(1);
  f.w.unmount();
});
it.each(["conflict", "malformed", "network"])(
  "%s results require explicit reread and never blind retry",
  async (mode) => {
    const f = setup(async (_p, o) => {
      if (o?.method === "PATCH") {
        if (mode === "conflict") throw new ApiError("changed", 409, -1);
        if (mode === "network") throw new Error("lost");
        return { ...original, merchant_id: "2" };
      }
      return original;
    });
    await flushPromises();
    await f.w.get("form").trigger("submit");
    await f.w.get('[data-action="confirm"]').trigger("click");
    await flushPromises();
    expect(f.w.text()).not.toContain("保存成功");
    expect(f.w.find("form").exists()).toBe(false);
    expect(f.request).toHaveBeenCalledTimes(2);
    await f.w.get('[data-action="reload"]').trigger("click");
    await flushPromises();
    expect(f.request).toHaveBeenCalledTimes(3);
    f.w.unmount();
  },
);
it("pending saves block duplicates and stale raw-client stamps discard late results", async () => {
  let resolve!: (x: unknown) => void;
  const d = new Promise((r) => (resolve = r)),
    f = setup(async (_p, o) => (o?.method === "PATCH" ? d : original));
  await flushPromises();
  await f.w.get("form").trigger("submit");
  await f.w.get('[data-action="confirm"]').trigger("click");
  await f.w.get("form").trigger("submit");
  expect(f.request).toHaveBeenCalledTimes(2);
  expect(
    f.w.get('[data-action="reload"]').attributes("disabled"),
  ).toBeDefined();
  f.c.csrfToken = "d".repeat(43);
  resolve({
    ...original,
    configured: true,
    revision: "1",
    fingerprint: "b".repeat(64),
  });
  await flushPromises();
  expect(f.w.find("form").exists()).toBe(false);
  expect(f.w.text()).not.toContain("保存成功");
  f.w.unmount();
});
it("keyboard confirmation focuses its action and Escape returns to the original save control", async () => {
  const c = new ApiClient("merchant");
  c.csrfToken = "c".repeat(43);
  vi.spyOn(c, "request").mockResolvedValue(original as never);
  const w = mount(Panel, {
    props: { client: c, merchantId: mid, ownSession: true },
    attachTo: document.body,
  });
  await flushPromises();
  const save = w.get('form button[type="submit"]');
  (save.element as HTMLElement).focus();
  await save.trigger("click");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(document.activeElement).toBe(w.get('[data-action="confirm"]').element);
  await w
    .get('[aria-label="确认功能开关"]')
    .trigger("keydown", { key: "Escape" });
  await flushPromises();
  expect(document.activeElement).toBe(
    w.get('form button[type="submit"]').element,
  );
  w.unmount();
});
