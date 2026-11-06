import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Card from "../src/components/ReferralCurrentPolicyCard.vue";
import policies from "../e2e/referral170-policies.json";

it("referral170 delayed policy cannot replace a later client or merchant context", async () => {
  let finish!: (v: unknown) => void;
  const old = new ApiClient("merchant"),
    current = new ApiClient("merchant");
  old.csrfToken = "old170";
  current.csrfToken = "new170";
  vi.spyOn(old, "request").mockImplementation(
    () => new Promise((r) => (finish = r)) as never,
  );
  const read = vi.spyOn(current, "request").mockResolvedValue({
    ...policies[6],
    merchant_id: "9007199254740993",
  } as never);
  const view = mount(Card, { props: { client: old, merchantID: "1" } });
  await view.setProps({ client: current, merchantID: "9007199254740993" });
  await flushPromises();
  expect(view.text()).toContain("当前未开启手续费预算");
  finish(policies[0]);
  await flushPromises();
  expect(view.text()).toContain("当前未开启手续费预算");
  expect(view.text()).not.toContain("当前通用政策已开启");
  expect(read.mock.calls[0]![0]).toBe("/referral/current-policy");
  expect(
    read.mock.calls.every(([, o]) => !o?.method || o.method === "GET"),
  ).toBe(true);
  view.unmount();
});
it("referral170 an unmounted request cannot render, and administrator clients never request merchant policy", async () => {
  let finish!: (v: unknown) => void;
  const c = new ApiClient("merchant");
  vi.spyOn(c, "request").mockImplementation(
    () => new Promise((r) => (finish = r)) as never,
  );
  const view = mount(Card, { props: { client: c, merchantID: "1" } });
  view.unmount();
  finish(policies[0]);
  await flushPromises();
  expect(view.emitted()).toEqual({});
  const admin = new ApiClient("admin"),
    read = vi.spyOn(admin, "request");
  const a = mount(Card, { props: { client: admin, merchantID: "1" } });
  await flushPromises();
  expect(read).not.toHaveBeenCalled();
  expect(a.text()).toContain("仅商户本人");
  a.unmount();
});
it("referral170 a foreign or malformed policy is isolated from the original referral history", async () => {
  const c = new ApiClient("merchant");
  vi.spyOn(c, "request").mockResolvedValue({
    ...policies[0],
    merchant_id: "2",
    token: "must-not-render",
  } as never);
  const view = mount(Card, { props: { client: c, merchantID: "1" } });
  await flushPromises();
  expect(view.get('[role="alert"]').text()).toContain("本人归属无法确认");
  expect(view.text()).not.toContain("33.33%");
  expect(view.text()).not.toContain("must-not-render");
  view.unmount();
});
