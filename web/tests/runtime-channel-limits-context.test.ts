import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import RuntimeChannelPolicyPanel from "../src/components/RuntimeChannelPolicyPanel.vue";
import { ApiClient } from "../src/api";
const firstID = "9007199254740993",
  nextID = "9007199254740995";
const catalogState = (id: string, rev = "1", fp = "a") => ({
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
const state = (id: string, rev = "1", fp = "a") => ({
  channel_id: id,
  pay_type: "alipay",
  configured: true,
  revision: rev,
  fingerprint: fp.repeat(64),
  can_update: true,
  timezone: "UTC+08:00",
  semantics: "successful-threshold-v1",
  daytop: "100.00",
  daymaxorder: "2",
  paymin: "0.01",
  paymax: "100.00",
  timestart: "",
  timestop: "",
  rate_limit_seconds: "30",
  rate_limit_count: "3",
  usage: {
    day: "2026-10-26",
    successful_amount: "0.00",
    successful_count: "0",
    recent_starts: "0",
    day_paused: false,
    rate_limited: false,
    as_of: "2026-10-26T06:00:00Z",
  },
});
function client(request: ReturnType<typeof vi.fn>) {
  const c = new ApiClient("admin");
  c.csrfToken = "context168";
  c.request = request as unknown as ApiClient["request"];
  return c;
}
it("a late global limits detail cannot reopen the previous session modal", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    first = vi
      .fn()
      .mockResolvedValueOnce({ items: [catalogState(firstID)], total: 1 })
      .mockReturnValueOnce(pending),
    next = vi
      .fn()
      .mockResolvedValue({ items: [catalogState(nextID)], total: 1 });
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: client(first), allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "限额设置")!
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
      .mockResolvedValueOnce({ items: [catalogState(firstID)], total: 1 })
      .mockResolvedValueOnce(before)
      .mockReturnValueOnce(pending),
    next = vi
      .fn()
      .mockResolvedValue({ items: [catalogState(nextID)], total: 1 });
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: client(first), allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "限额设置")!
    .trigger("click");
  await flushPromises();
  await w.get("input").setValue("98.75");
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
  expect(w.text()).not.toContain("限额设置已保存");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(next).toHaveBeenCalledTimes(1);
  w.unmount();
});

it("an in-place nonreactive CSRF change invalidates a pending detail read", async () => {
  let resolve = (_: unknown) => {};
  const pending = new Promise((r) => (resolve = r)),
    request = vi
      .fn()
      .mockResolvedValueOnce({ items: [catalogState(firstID)], total: 1 })
      .mockReturnValueOnce(pending),
    api = client(request);
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: api, allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "限额设置")!
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
      .mockResolvedValueOnce({ items: [catalogState(firstID)], total: 1 })
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
    .find((b) => b.text() === "限额设置")!
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
      .mockResolvedValueOnce({ items: [catalogState(firstID)], total: 1 })
      .mockResolvedValueOnce(before)
      .mockReturnValueOnce(pending),
    api = client(request);
  const w = mount(RuntimeChannelPolicyPanel, {
    props: { client: api, allowed: true },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "限额设置")!
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
  expect(w.text()).not.toContain("限额设置已保存");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(request).toHaveBeenCalledTimes(3);
  w.unmount();
});
