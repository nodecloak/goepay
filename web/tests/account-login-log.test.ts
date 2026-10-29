// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import AccountLoginLogDialog from "../src/components/AccountLoginLogDialog.vue";
const row = {
  id: "1",
  type: "",
  city: "",
  ip: "192.0.2.121",
  created_at: "2026-10-25T00:00:00Z",
};

it("admin target logs use an explicit merchant path and discard a late previous merchant response", async () => {
  const client = new ApiClient("admin");
  let finish!: (value: unknown) => void;
  const calls = vi.spyOn(client, "request").mockImplementation((path) =>
    path.includes("/1/")
      ? (new Promise((resolve) => {
          finish = resolve;
        }) as never)
      : (Promise.resolve({
          items: [{ ...row, id: "2", ip: "192.0.2.2" }],
          total: 1,
        }) as never),
  );
  const w = mount(AccountLoginLogDialog, {
    props: { client, ownSession: true, merchantID: "1" },
  });
  await flushPromises();
  expect(calls.mock.calls[0]?.[0]).toBe(
    "/merchants/1/login-log?page=1&limit=20",
  );
  await w.setProps({ merchantID: "2" });
  await flushPromises();
  finish({ items: [row], total: 1 });
  await flushPromises();
  expect(w.text()).toContain("商户 2");
  expect(w.text()).toContain("192.0.2.2");
  expect(w.text()).not.toContain("192.0.2.121");
  await w.setProps({ ownSession: false });
  await flushPromises();
  expect(w.text()).not.toContain("192.0.2.2");
  expect(calls).toHaveBeenCalledTimes(2);
  w.unmount();
});

it("merchant clients cannot turn their own log dialog into an administrator target lookup", async () => {
  const client = new ApiClient("merchant"),
    calls = vi.spyOn(client, "request");
  const w = mount(AccountLoginLogDialog, {
    props: { client, ownSession: true, merchantID: "2" },
  });
  await flushPromises();
  expect(calls).not.toHaveBeenCalled();
  expect(w.get('[role="alert"]').text()).toContain("管理员");
  w.unmount();
});
it("renders original log columns with unknown facts and uses only canonical GET pagination", async () => {
  const client = new ApiClient("merchant"),
    calls = vi
      .spyOn(client, "request")
      .mockResolvedValue({ items: [row], total: 21 } as never);
  const w = mount(AccountLoginLogDialog, {
    props: { client, ownSession: true },
    attachTo: document.body,
  });
  await flushPromises();
  expect(w.findAll("th").map((h) => h.text())).toEqual([
    "登录方式",
    "IP地址",
    "归属地",
    "时间",
  ]);
  expect(w.text()).toContain("192.0.2.121");
  expect(w.text()).toContain("2026-10-25 08:00:00");
  expect(w.findAll("td").filter((c) => c.text() === "未记录")).toHaveLength(2);
  await w.find('.pa-page[aria-label="下一页"]').trigger("click");
  await flushPromises();
  expect(calls.mock.calls.at(-1)?.[0]).toBe(
    "/account/login-log?page=2&limit=20",
  );
  expect(calls.mock.calls.every(([, options]) => !options?.method)).toBe(true);
  await w.find('[role="dialog"]').trigger("keydown", { key: "Escape" });
  expect(w.emitted("close")).toHaveLength(1);
  w.unmount();
});
it("never reads delegated logs and discards a late previous-account response", async () => {
  const old = new ApiClient("merchant"),
    current = new ApiClient("merchant");
  let finish!: (value: unknown) => void;
  const calls = vi.spyOn(old, "request").mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }) as never,
  );
  const currentCalls = vi.spyOn(current, "request").mockResolvedValue({
    items: [{ ...row, id: "2", ip: "192.0.2.122" }],
    total: 1,
  } as never);
  const w = mount(AccountLoginLogDialog, {
    props: { client: old, ownSession: true },
  });
  await flushPromises();
  await w.setProps({ client: current });
  await flushPromises();
  expect(calls.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
  finish({ items: [row], total: 1 });
  await flushPromises();
  expect(w.text()).toContain("192.0.2.122");
  expect(w.text()).not.toContain("192.0.2.121");
  await w.setProps({ ownSession: false });
  await flushPromises();
  expect(w.text()).not.toContain("192.0.2.122");
  expect(w.text()).toContain("仅本人会话");
  expect(currentCalls).toHaveBeenCalledTimes(1);
  w.unmount();
});
it("a malformed response clears rows rather than showing a previous account record", async () => {
  const client = new ApiClient("merchant");
  vi.spyOn(client, "request").mockResolvedValue({
    items: [row],
    total: "1",
  } as never);
  const w = mount(AccountLoginLogDialog, {
    props: { client, ownSession: true },
  });
  await flushPromises();
  expect(w.text()).toContain("登录日志响应格式无效");
  expect(w.text()).not.toContain("192.0.2.121");
  w.unmount();
});
