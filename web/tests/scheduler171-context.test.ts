import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import TaskSchedulerPage from "../src/components/TaskSchedulerPage.vue";
import { ApiClient, ApiError } from "../src/api";
import { scheduler171Task, scheduler171Run } from "./scheduler171-fixtures";

function client(request: ReturnType<typeof vi.fn>) {
  const c = new ApiClient("admin");
  c.csrfToken = "synthetic171";
  c.request = request as unknown as ApiClient["request"];
  return c;
}
const catalog = (items = [scheduler171Task()]) => ({
  items,
  total: items.length,
});
const button = (w: ReturnType<typeof mount>, name: string) => {
  const found = w.findAll("button").find((b) => b.text() === name);
  if (!found) throw Error("missing button " + name);
  return found;
};
it("permission prevents even catalog reads and original legacy mode is never labeled disabled", async () => {
  const request = vi
    .fn()
    .mockResolvedValue(
      catalog([
        scheduler171Task({
          task: "notify",
          configured: false,
          control_mode: "legacy_managed",
          enabled: false,
          schedule: null,
          params: {},
          parameter_definitions: [],
          revision: "0",
          can_run: false,
          next_run_time: null,
        }),
      ]),
    );
  const w = mount(TaskSchedulerPage, {
    props: { client: client(request), allowed: false },
  });
  await flushPromises();
  expect(request).not.toHaveBeenCalled();
  expect(w.text()).toContain("超级管理员");
  await w.setProps({ allowed: true });
  await flushPromises();
  expect(w.text()).toContain("未接管");
  expect(w.text()).not.toContain("已关闭");
  w.unmount();
});
it("configuration cancellation sends no PATCH and returns focus to its opener", async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce(catalog())
    .mockResolvedValueOnce(scheduler171Task());
  const w = mount(TaskSchedulerPage, {
    attachTo: document.body,
    props: { client: client(request), allowed: true },
  });
  await flushPromises();
  const opener = button(w, "配置");
  await opener.trigger("click");
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(true);
  await w.find("form").trigger("submit");
  await flushPromises();
  expect(button(w, "确认保存").exists()).toBe(true);
  await button(w, "取消").trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
  ).toHaveLength(0);
  expect(document.activeElement).toBe(opener.element);
  w.unmount();
});
it("manual run only announces the actual large pending intent and does not claim execution completed", async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce(catalog())
    .mockResolvedValueOnce(scheduler171Task())
    .mockResolvedValueOnce(scheduler171Run());
  const w = mount(TaskSchedulerPage, {
    props: { client: client(request), allowed: true },
  });
  await flushPromises();
  await button(w, "立即运行").trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  await button(w, "确认安排").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("9007199254740993");
  expect(w.text()).toContain("等待调度器执行");
  expect(w.text()).not.toContain("执行成功");
  expect(request.mock.calls[2][1].body).toEqual({
    expected_fingerprint: "a".repeat(64),
  });
  w.unmount();
});
it("same-instance unobserved CSRF replacement invalidates old confirmation without POST", async () => {
  const request = vi
      .fn()
      .mockResolvedValueOnce(catalog())
      .mockResolvedValueOnce(scheduler171Task()),
    c = client(request);
  const w = mount(TaskSchedulerPage, { props: { client: c, allowed: true } });
  await flushPromises();
  await button(w, "立即运行").trigger("click");
  await flushPromises();
  c.csrfToken = "replaced171";
  await button(w, "确认安排").trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(2);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).toContain("会话已变更");
  w.unmount();
});
it("late detail cannot overwrite the next session", async () => {
  let finish = (_: unknown) => {};
  const pending = new Promise((r) => (finish = r));
  const first = vi
      .fn()
      .mockResolvedValueOnce(catalog())
      .mockReturnValueOnce(pending),
    second = vi
      .fn()
      .mockResolvedValue(catalog([scheduler171Task({ name: "新会话任务" })]));
  const w = mount(TaskSchedulerPage, {
    props: { client: client(first), allowed: true },
  });
  await flushPromises();
  await button(w, "配置").trigger("click");
  await w.setProps({ client: client(second) });
  await flushPromises();
  finish(scheduler171Task({ name: "旧会话不得出现" }));
  await flushPromises();
  expect(w.text()).toContain("新会话任务");
  expect(w.text()).not.toContain("旧会话不得出现");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  w.unmount();
});
it("late committed manual run after an unobserved token replacement cannot report success or retry", async () => {
  let finish = (_: unknown) => {};
  const pending = new Promise((r) => (finish = r)),
    request = vi
      .fn()
      .mockResolvedValueOnce(catalog())
      .mockResolvedValueOnce(scheduler171Task())
      .mockReturnValueOnce(pending),
    c = client(request);
  const w = mount(TaskSchedulerPage, { props: { client: c, allowed: true } });
  await flushPromises();
  await button(w, "立即运行").trigger("click");
  await flushPromises();
  await button(w, "确认安排").trigger("click");
  await flushPromises();
  c.csrfToken = "next171-session";
  finish(scheduler171Run());
  await flushPromises();
  expect(w.text()).not.toContain("已安排");
  expect(w.text()).toContain("会话已变更");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(
    request.mock.calls.filter((c) => c[1]?.method === "POST"),
  ).toHaveLength(1);
  w.unmount();
});
it("a mismatched detail cannot provide authority and a terminal manual response is held uncertain", async () => {
  const mismatch = vi
    .fn()
    .mockResolvedValueOnce(catalog())
    .mockResolvedValueOnce(scheduler171Task({ task: "settlement_generate" }));
  const first = mount(TaskSchedulerPage, {
    props: { client: client(mismatch), allowed: true },
  });
  await flushPromises();
  await button(first, "配置").trigger("click");
  await flushPromises();
  expect(first.text()).toContain("任务详情读取失败");
  expect(first.find("form").exists()).toBe(false);
  first.unmount();
  const request = vi
    .fn()
    .mockResolvedValueOnce(catalog())
    .mockResolvedValueOnce(scheduler171Task())
    .mockResolvedValueOnce(
      scheduler171Run({
        status: "success",
        started_at: "2026-10-26T02:00:00Z",
        completed_at: "2026-10-26T02:00:01Z",
        counts: {
          examined: 0,
          awarded: 0,
          generated: 0,
          skipped: 0,
          enqueued: 0,
          awaiting_authorization: 0,
        },
      }),
    );
  const w = mount(TaskSchedulerPage, {
    props: { client: client(request), allowed: true },
  });
  await flushPromises();
  await button(w, "立即运行").trigger("click");
  await flushPromises();
  await button(w, "确认安排").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("安排结果尚未确认");
  expect(w.text()).not.toContain("已安排");
  expect(
    request.mock.calls.filter((c) => c[1]?.method === "POST"),
  ).toHaveLength(1);
  w.unmount();
});
it("busy request cannot be dismissed and uncertain result requires fresh GET, never automatic replay", async () => {
  let reject = (_: unknown) => {};
  const pending = new Promise((_, r) => (reject = r)),
    request = vi
      .fn()
      .mockResolvedValueOnce(catalog())
      .mockResolvedValueOnce(scheduler171Task())
      .mockReturnValueOnce(pending)
      .mockResolvedValueOnce(
        scheduler171Task({
          fingerprint: "b".repeat(64),
          revision: "9007199254740994",
        }),
      );
  const w = mount(TaskSchedulerPage, {
    props: { client: client(request), allowed: true },
  });
  await flushPromises();
  await button(w, "配置").trigger("click");
  await flushPromises();
  await w.find("form").trigger("submit");
  await button(w, "确认保存").trigger("click");
  await flushPromises();
  expect(button(w, "关闭").attributes("disabled")).toBeDefined();
  await w.find('[role="dialog"]').trigger("keydown", { key: "Escape" });
  expect(w.find('[role="dialog"]').exists()).toBe(true);
  reject(new ApiError("synthetic", 503, -1));
  await flushPromises();
  expect(w.text()).toContain("结果尚未确认");
  expect(
    request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
  ).toHaveLength(1);
  await button(w, "刷新核对").trigger("click");
  await flushPromises();
  expect(w.text()).not.toContain("结果尚未确认");
  expect(
    request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
  ).toHaveLength(1);
  w.unmount();
});
it("history binds full cursor and rejects a different task instead of rendering it", async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce(catalog())
    .mockResolvedValueOnce(scheduler171Task())
    .mockResolvedValueOnce({
      items: [scheduler171Run()],
      total: 2,
      next_cursor: "9007199254740993",
    })
    .mockResolvedValueOnce({
      items: [scheduler171Run({ task: "notify", id: "9007199254740992" })],
      total: 1,
      next_cursor: "0",
    });
  const w = mount(TaskSchedulerPage, {
    props: { client: client(request), allowed: true },
  });
  await flushPromises();
  await button(w, "执行记录").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("9007199254740993");
  await button(w, "下一页").trigger("click");
  await flushPromises();
  expect(request.mock.calls.at(-1)?.[0]).toBe(
    "/scheduled-tasks/referral_daily/runs?limit=20&before_id=9007199254740993",
  );
  expect(w.text()).not.toContain("9007199254740992");
  expect(w.text()).toContain("执行记录读取失败");
  w.unmount();
});
