// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ChannelStopPanel from "../src/components/ChannelStopPanel.vue";

const stopped = {
  channel_id: "9007199254740993",
  event_id: "9007199254741001",
  revision: "1",
  blocked: true,
  code: "40004:PAYER_BALANCE_NOT_ENOUGH",
  changed_at: "2026-10-24T00:00:00Z",
  provider: "alipay",
};
it("loads only current runtime status and requires explicit selection and confirmation before exact restoration", async () => {
  const client = new ApiClient("admin");
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ items: [stopped] });
  const w = mount(ChannelStopPanel, { props: { client } });
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
  expect(request.mock.calls[0]?.[0]).toBe("/channel-stops");
  expect(w.text()).toContain("已停用");
  expect(w.text()).toContain(stopped.channel_id);
  await w
    .findAll("button")
    .find((b) => b.text() === "恢复通道")!
    .trigger("click");
  expect(request).toHaveBeenCalledTimes(1);
  const confirm = w.findAll("button").find((b) => b.text() === "确认恢复")!;
  expect(confirm.attributes("disabled")).toBeDefined();
  await w.get('input[type="checkbox"]').setValue(true);
  request.mockResolvedValue({
    ...stopped,
    blocked: false,
    revision: "2",
    event_id: "9007199254741002",
  });
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  expect(request.mock.calls[1]?.[0]).toBe(
    "/channel-stops/9007199254740993/resume",
  );
  expect(request.mock.calls[1]?.[1]).toMatchObject({
    method: "POST",
    body: { expected_event_id: "9007199254741001", expected_revision: "1" },
  });
  expect(w.text()).toContain("已恢复");
  w.unmount();
});

it("clears stale choices after read failure and never retries a rejected restoration automatically", async () => {
  const client = new ApiClient("admin");
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ items: [stopped] });
  const w = mount(ChannelStopPanel, { props: { client } });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "恢复通道")!
    .trigger("click");
  await w.get('input[type="checkbox"]').setValue(true);
  request.mockRejectedValue(new Error("本地测试：请重新核对状态"));
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  expect(w.text()).toContain("请重新核对状态");
  expect(
    w.findAll("button").filter((b) => b.text() === "确认恢复"),
  ).toHaveLength(0);
  await w
    .findAll("button")
    .find((b) => b.text() === "刷新状态")!
    .trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(3);
  expect(w.text()).not.toContain(stopped.channel_id);
  w.unmount();
});

it("rejects numeric or contradictory status IDs before offering a restore action", async () => {
  for (const changed of [
    { channel_id: 9007199254740992 },
    { event_id: 31 },
    { revision: 1 },
    { blocked: true, event_id: "0" },
    { notice_state: "delivered" },
  ]) {
    const client = new ApiClient("admin");
    vi.spyOn(client, "request").mockResolvedValue({
      items: [{ ...stopped, ...changed }],
    });
    const w = mount(ChannelStopPanel, { props: { client } });
    await flushPromises();
    expect(w.text()).toContain("响应不完整");
    expect(w.findAll("button").some((b) => b.text() === "恢复通道")).toBe(
      false,
    );
    w.unmount();
  }
});

it("discards an in-flight restoration when the active console client changes", async () => {
  const first = new ApiClient("admin");
  let finish: (value: unknown) => void = () => {};
  vi.spyOn(first, "request").mockImplementation(async (_path, options) =>
    options?.method === "POST"
      ? ((await new Promise<unknown>((resolve) => {
          finish = resolve;
        })) as never)
      : ({ items: [stopped] } as never),
  );
  const next = new ApiClient("admin");
  const read = vi.spyOn(next, "request").mockResolvedValue({ items: [] });
  const w = mount(ChannelStopPanel, { props: { client: first } });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "恢复通道")!
    .trigger("click");
  await w.get('input[type="checkbox"]').setValue(true);
  await w.get("form").trigger("submit");
  await w.setProps({ client: next });
  await flushPromises();
  finish({ ...stopped, blocked: false, event_id: "32", revision: "2" });
  await flushPromises();
  expect(read).toHaveBeenCalledTimes(1);
  expect(w.text()).not.toContain(stopped.channel_id);
  expect(w.text()).not.toContain("通道已恢复");
  w.unmount();
});

it("shows the proven notification transport and service acceptance without claiming delivery", async () => {
  for (const [transport, label] of [
    ["smtp", "邮件"],
    ["robot", "群机器人"],
    ["telegram", "Telegram"],
    ["qqbot", "QQ机器人"],
  ]) {
    const client = new ApiClient("admin");
    vi.spyOn(client, "request").mockResolvedValue({
      items: [
        { ...stopped, notice_state: "accepted", notice_transport: transport },
      ],
    });
    const w = mount(ChannelStopPanel, { props: { client } });
    await flushPromises();
    expect(w.text()).toContain(`通知方式：${label}`);
    expect(w.text()).toContain("通知服务已受理");
    expect(w.text()).not.toContain("已送达");
    w.unmount();
  }
});

it("rejects an unknown notification transport before displaying an actionable status", async () => {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockResolvedValue({
    items: [
      { ...stopped, notice_state: "accepted", notice_transport: "unproven" },
    ],
  });
  const w = mount(ChannelStopPanel, { props: { client } });
  await flushPromises();
  expect(w.text()).toContain("响应不完整");
  expect(w.findAll("button").some((b) => b.text() === "恢复通道")).toBe(false);
  w.unmount();
});
