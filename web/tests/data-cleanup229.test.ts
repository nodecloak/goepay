import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import OperationsPage from "../src/components/OperationsPage.vue";
import { ApiClient, ApiError } from "../src/api";

const date = "2026-10-03";
const cutoff = date + "T00:00:00+08:00";
const report = (preview = true) => ({
  cutoff,
  preview,
  expired_sessions: 2,
  expired_attempts: 3,
  expired_login_throttle_keys: 5,
  expired_login_audit_rows: 7,
});
let mounted: ReturnType<typeof mount>[] = [];
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-02T06:00:00Z"));
});
afterEach(() => {
  for (const w of mounted) w.unmount();
  mounted = [];
  document.body.innerHTML = "";
  vi.useRealTimers();
});
function setup(
  handler?: (path: string, options?: any) => Promise<unknown>,
  actor = "super",
) {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-clean229";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(
      handler || (async (_p, o) => report((o?.body as any)?.preview)),
    );
  const w = mount(OperationsPage, {
    props: {
      client,
      operation: "cleanup",
      ownSession: true,
      adminClass: actor,
    },
    attachTo: document.body,
    global: { stubs: { Teleport: true } },
  });
  mounted.push(w);
  return { w, request, client };
}
function button(w: ReturnType<typeof mount>, name: string) {
  const b = w.findAll("button").find((x) => x.text() === name);
  if (!b) throw Error("Missing button " + name);
  return b;
}
async function preview(w: ReturnType<typeof mount>) {
  await w.get('input[type="date"]').setValue(date);
  await button(w, "预览清理范围").trigger("click");
  await flushPromises();
}

it("shows all four real cleanup counts and the exact reviewed cutoff", async () => {
  const { w, request } = setup();
  await preview(w);
  expect(w.text()).toContain("持久限流记录");
  for (const [key, count] of Object.entries(report()).filter(([k]) =>
    k.startsWith("expired_"),
  ))
    expect(w.get(`[data-cleanup-count="${key}"]`).text()).toBe(String(count));
  expect(request).toHaveBeenCalledWith(
    "/operations/cleanup",
    expect.objectContaining({ body: { preview: true, cutoff } }),
  );
  expect(w.text()).toContain("2026-10-03");
});

it("canceling the reviewed deletion sends nothing and returns focus", async () => {
  const { w, request } = setup();
  await preview(w);
  const opener = button(w, "立即删除");
  (opener.element as HTMLElement).focus();
  await opener.trigger("click");
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(true);
  expect(w.get('[role="dialog"]').text()).toContain("7");
  await button(w, "取消").trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => (o?.body as any)?.preview === false),
  ).toHaveLength(0);
  expect(document.activeElement).toBe(opener.element);
});

it("reports the actual returned deletion counts rather than only session cleanup", async () => {
  const { w, request } = setup();
  await preview(w);
  await button(w, "立即删除").trigger("click");
  await button(w, "确认清理").trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => (o?.body as any)?.preview === false),
  ).toHaveLength(1);
  expect(w.get('[role="status"]').text()).toContain("登录日志 7 条");
  expect(w.get('[role="status"]').text()).toContain("持久限流记录 5 条");
  expect(w.find('[role="dialog"]').exists()).toBe(false);
});

it.each([
  {},
  { ...report(), expired_login_audit_rows: "7" },
  { ...report(), cutoff: "2026-10-01T00:00:00+08:00" },
  { ...report(), preview: false },
])(
  "rejects an incomplete or different preview without exposing delete",
  async (data) => {
    const { w } = setup(async () => data);
    await preview(w);
    expect(w.find('[role="alert"]').exists()).toBe(true);
    expect(w.findAll("button").some((b) => b.text() === "立即删除")).toBe(
      false,
    );
  },
);

it.each(["operator", "readonly"])(
  "does not expose writable cleanup for %s",
  async (actor) => {
    const { w, request } = setup(undefined, actor);
    await flushPromises();
    expect(
      w
        .findAll("button")
        .filter(
          (b) =>
            b.attributes("disabled") === undefined &&
            /预览清理范围|立即删除|清理运行日志/.test(b.text()),
        ),
    ).toHaveLength(0);
    expect(request).not.toHaveBeenCalled();
  },
);

it("locks a lost deletion result until an explicit fresh preview and never auto-replays", async () => {
  const { w, request } = setup(async (_p, o) => {
    if ((o?.body as any)?.preview === false)
      throw new ApiError("damaged reply", 200, -1, undefined, false);
    return report();
  });
  await preview(w);
  await button(w, "立即删除").trigger("click");
  await button(w, "确认清理").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("清理结果尚未核实");
  expect(w.findAll("button").some((b) => b.text() === "立即删除")).toBe(false);
  expect(
    request.mock.calls.filter(([, o]) => (o?.body as any)?.preview === false),
  ).toHaveLength(1);
  await button(w, "重新预览核对").trigger("click");
  await flushPromises();
  expect(button(w, "立即删除").exists()).toBe(true);
  expect(
    request.mock.calls.filter(([, o]) => (o?.body as any)?.preview === false),
  ).toHaveLength(1);
});

it("locks the date during preview and invalidates the completed range when edited", async () => {
  let release!: (v: unknown) => void;
  const { w } = setup(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  await w.get('input[type="date"]').setValue(date);
  await button(w, "预览清理范围").trigger("click");
  expect(
    (w.get('input[type="date"]').element as HTMLInputElement).disabled,
  ).toBe(true);
  release(report());
  await flushPromises();
  expect(button(w, "立即删除").exists()).toBe(true);
  await w.get('input[type="date"]').setValue("2026-10-04");
  expect(w.findAll("button").some((b) => b.text() === "立即删除")).toBe(false);
});

it("revoking the actor while reading removes the pending preview", async () => {
  let release!: (v: unknown) => void;
  const { w } = setup(
    () =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  await w.get('input[type="date"]').setValue(date);
  await button(w, "预览清理范围").trigger("click");
  await w.setProps({ adminClass: "operator" });
  release(report());
  await flushPromises();
  expect(w.findAll("button").some((b) => b.text() === "立即删除")).toBe(false);
  expect(w.find("[data-cleanup-count]").exists()).toBe(false);
});

it("a raw CSRF replacement cannot consume the old preview", async () => {
  const { w, client, request } = setup();
  await preview(w);
  await button(w, "立即删除").trigger("click");
  client.csrfToken = "replacement-clean229";
  await button(w, "确认清理").trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => (o?.body as any)?.preview === false),
  ).toHaveLength(0);
});
