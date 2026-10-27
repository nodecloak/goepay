import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import RuntimeChannelPolicyPanel from "../src/components/RuntimeChannelPolicyPanel.vue";
import { ApiClient } from "../src/api";
const firstID = "9007199254740993",
  nextID = "9007199254740995";
const state = (id: string, rev = "1", fp = "a") => ({
  channel_id: id,
  pay_type: "alipay",
  methods: ["page"],
  configured: true,
  rate: "99.50",
  fee_rate_bps: 50,
  mode: 0,
  enabled: true,
  revision: rev,
  fingerprint: fp.repeat(64),
  can_update: true,
});
function client(request: (path: string, options?: unknown) => unknown) {
  const c = new ApiClient("admin");
  c.csrfToken = "context168";
  // The new independent minimum-fee panel owns another typed GET. Keep the
  // original retained-rate send/late-reply sequence and all its assertions.
  c.request = ((path: string, options?: unknown) =>
    path.startsWith("/buyer-fees/parents/")
      ? Promise.resolve({
          kind: "parent",
          channel_id: path.split("/").at(-1),
          payfee_lessthan: "",
          payfee_mincost: "",
          effective_payfee_lessthan: "0.00",
          effective_payfee_mincost: "0.00",
          configured: false,
          revision: "0",
          fingerprint: "d".repeat(64),
          can_update: true,
        })
      : request(path, options)) as ApiClient["request"];
  return c;
}
it("a late channel detail cannot reopen the previous session modal", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    first = vi
      .fn()
      .mockResolvedValueOnce({ items: [state(firstID)], total: 1 })
      .mockReturnValueOnce(pending),
    next = vi.fn().mockResolvedValue({ items: [state(nextID)], total: 1 });
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: client(first), allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "设置费率")!
    .trigger("click");
  await w.setProps({ client: client(next) });
  await flushPromises();
  resolve(state(firstID));
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).toContain(nextID);
  expect(w.text()).not.toContain(firstID);
  expect(next).toHaveBeenCalledTimes(1);
  w.unmount();
});
it("a late committed policy response cannot mark another session configuration saved", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    before = state(firstID),
    first = vi
      .fn()
      .mockResolvedValueOnce({ items: [before], total: 1 })
      .mockResolvedValueOnce(before)
      .mockReturnValueOnce(pending),
    next = vi.fn().mockResolvedValue({ items: [state(nextID)], total: 1 });
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: client(first), allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "设置费率")!
    .trigger("click");
  await flushPromises();
  await w.get("#runtime-channel-rate").setValue("98.75");
  await w.get("form").trigger("submit");
  await w
    .findAll("button")
    .find((b) => b.text() === "确定保存")!
    .trigger("click");
  await flushPromises();
  expect(first).toHaveBeenCalledTimes(3);
  await w.setProps({ client: client(next) });
  await flushPromises();
  resolve({
    ...before,
    rate: "98.75",
    fee_rate_bps: 125,
    revision: "2",
    fingerprint: "b".repeat(64),
  });
  await flushPromises();
  expect(w.text()).toContain(nextID);
  expect(w.text()).not.toContain(firstID);
  expect(w.text()).not.toContain("费率设置已保存");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(next).toHaveBeenCalledTimes(1);
  w.unmount();
});

it("an in-place nonreactive CSRF change invalidates a pending detail read", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    request = vi
      .fn()
      .mockResolvedValueOnce({ items: [state(firstID)], total: 1 })
      .mockReturnValueOnce(pending),
    api = client(request);
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: api, allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "设置费率")!
    .trigger("click");
  api.csrfToken = "another-session";
  resolve(state(firstID));
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).not.toContain(firstID);
  expect(request).toHaveBeenCalledTimes(2);
  w.unmount();
});

it("an in-place CSRF change before confirmation submit cannot authorize the old draft", async () => {
  const before = state(firstID),
    request = vi
      .fn()
      .mockResolvedValueOnce({ items: [before], total: 1 })
      .mockResolvedValueOnce(before)
      .mockResolvedValueOnce({
        ...before,
        revision: "2",
        fingerprint: "b".repeat(64),
      }),
    api = client(request);
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: api, allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "设置费率")!
    .trigger("click");
  await flushPromises();
  await w.get("form").trigger("submit");
  api.csrfToken = "another-session";
  await w
    .findAll("button")
    .find((b) => b.text() === "确定保存")!
    .trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  w.unmount();
});

it("an in-place CSRF change discards an already-sent response without marking it saved", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    before = state(firstID),
    request = vi
      .fn()
      .mockResolvedValueOnce({ items: [before], total: 1 })
      .mockResolvedValueOnce(before)
      .mockReturnValueOnce(pending),
    api = client(request);
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: api, allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "设置费率")!
    .trigger("click");
  await flushPromises();
  await w.get("form").trigger("submit");
  await w
    .findAll("button")
    .find((b) => b.text() === "确定保存")!
    .trigger("click");
  await flushPromises();
  api.csrfToken = "another-session";
  resolve({ ...before, revision: "2", fingerprint: "b".repeat(64) });
  await flushPromises();
  expect(w.text()).not.toContain("费率设置已保存");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(request).toHaveBeenCalledTimes(3);
  w.unmount();
});
