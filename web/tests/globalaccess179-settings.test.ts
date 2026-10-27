import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";
import actual from "../e2e/globalaccess179-http-fixture.json";
import Panel from "../src/components/PlatformMerchantAccessSettings.vue";
import { ApiError } from "../src/api";

it("does not call the real typed platform control an empty settings category", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "original-context";
  client.request = vi.fn(async (path: string) => {
    if (path === "/resources/settings/schema")
      return {
        title: "设置",
        fields: [{ name: "key", type: "select", options: [] }],
        permissions: { create: true, update: true, delete: false },
      };
    if (path.startsWith("/resources/settings?")) return { items: [], total: 0 };
    if (path === "/settings/merchant-access") return actual.admin_closed;
    throw Error(`unexpected ${path}`);
  }) as ApiClient["request"];
  const w = mount(SettingsPage, {
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  expect(w.find('input[name="close_keylogin"]').exists()).toBe(true);
  expect(w.text()).not.toContain("该分类当前没有可在线维护的设置项");
  w.unmount();
});

it("uses the original site control and preserves the refund gate from the real HTTP state", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-settings-session";
  const request = vi.fn(
    async (path: string, options?: { method?: string; body?: unknown }) => {
      if (path === "/resources/settings/schema")
        return {
          title: "设置",
          fields: [{ name: "key", type: "select", options: ["site_name"] }],
          permissions: { create: true, update: true, delete: false },
        };
      if (path.startsWith("/resources/settings?"))
        return { items: [], total: 0 };
      if (path === "/settings/merchant-access")
        return options?.method === "PATCH"
          ? actual.admin_key_open_refund_closed
          : actual.admin_closed;
      throw Error(`unexpected ${path}`);
    },
  );
  client.request = request as ApiClient["request"];
  const w = mount(SettingsPage, {
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  expect(w.find('input[name="close_keylogin"]').exists()).toBe(true);
  await w.get('input[name="close_keylogin"]').setValue(true);
  await w.get("[data-platform-access-save]").trigger("click");
  await w.get("[data-platform-access-confirm]").trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    "/settings/merchant-access",
    expect.objectContaining({
      method: "PATCH",
      body: {
        close_keylogin: "0",
        user_refund: "0",
        expected_fingerprint: actual.admin_closed.fingerprint,
      },
    }),
  );
  expect(w.text()).toContain("平台功能设置已保存");
  w.unmount();
});

function panel(
  handler: (
    path: string,
    options?: { method?: string; body?: unknown },
  ) => Promise<unknown>,
  field: "close_keylogin" | "user_refund" = "user_refund",
) {
  const client = new ApiClient("admin");
  client.csrfToken = "original-context";
  const request = vi.fn(handler);
  client.request = request as ApiClient["request"];
  const w = mount(Panel, {
    props: { client, field, adminClass: "super", ownSession: true },
    attachTo: document.body,
  });
  return { client, request, w };
}
it("the merchant tab enables refunds without reopening the hidden key-login gate", async () => {
  const f = panel(async (_p, o) =>
    o?.method === "PATCH"
      ? actual.admin_key_closed_refund_open
      : actual.admin_closed,
  );
  await flushPromises();
  await f.w.get('input[name="user_refund"]').setValue(true);
  await f.w.get("[data-platform-access-save]").trigger("click");
  expect(document.activeElement).toBe(
    f.w.get("[data-platform-access-confirm]").element,
  );
  await f.w.get("[data-platform-access-confirm]").trigger("click");
  await flushPromises();
  expect(f.request.mock.calls[1]?.[1]?.body).toEqual({
    close_keylogin: "1",
    user_refund: "1",
    expected_fingerprint: actual.admin_closed.fingerprint,
  });
  expect(f.w.text()).toContain("平台功能设置已保存");
  f.w.unmount();
});
it.each(["lost", "conflict", "wrong-hidden-state"])(
  "%s requires a new read and never retries a write",
  async (mode) => {
    const f = panel(async (_p, o) => {
      if (o?.method !== "PATCH") return actual.admin_closed;
      if (mode === "conflict") throw new ApiError("stale", 409, -1);
      if (mode === "lost") throw Error("commit response lost");
      return actual.admin_default;
    });
    await flushPromises();
    await f.w.get("input").setValue(true);
    await f.w.get("[data-platform-access-save]").trigger("click");
    await f.w.get("[data-platform-access-confirm]").trigger("click");
    await flushPromises();
    expect(f.w.text()).not.toContain("已保存");
    expect(f.w.find("input").exists()).toBe(false);
    expect(
      f.request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
    ).toHaveLength(1);
    await f.w.get("button").trigger("click");
    await flushPromises();
    expect(
      f.request.mock.calls.filter((c) => c[1]?.method === "PATCH"),
    ).toHaveLength(1);
    f.w.unmount();
  },
);
it("raw same-client CSRF replacement clears the draft before confirmation can send", async () => {
  const f = panel(async () => actual.admin_closed);
  await flushPromises();
  await f.w.get("input").setValue(true);
  f.client.csrfToken = "another-context";
  await f.w.get("[data-platform-access-save]").trigger("click");
  await flushPromises();
  expect(f.w.find("input").exists()).toBe(false);
  expect(f.request).toHaveBeenCalledTimes(1);
  expect(f.w.text()).toContain("会话已变化");
  f.w.unmount();
});
it("delegation and operators do not read the privileged settings", async () => {
  const f = panel(async () => actual.admin_closed);
  await flushPromises();
  await f.w.setProps({ ownSession: false });
  await flushPromises();
  const calls = f.request.mock.calls.length;
  expect(f.w.find("input").exists()).toBe(false);
  await f.w.setProps({ ownSession: true, adminClass: "operator" });
  await flushPromises();
  expect(f.request).toHaveBeenCalledTimes(calls);
  f.w.unmount();
});
it("a late response cannot restore an old context or claim a saved setting", async () => {
  let resolve!: (v: unknown) => void;
  const f = panel(async (_p, o) =>
    o?.method === "PATCH"
      ? new Promise((r) => (resolve = r))
      : actual.admin_closed,
  );
  await flushPromises();
  await f.w.get("input").setValue(true);
  await f.w.get("[data-platform-access-save]").trigger("click");
  await f.w.get("[data-platform-access-confirm]").trigger("click");
  expect(f.w.emitted("busy")?.at(-1)).toEqual([true]);
  f.client.csrfToken = "new-context";
  resolve(actual.admin_key_closed_refund_open);
  await flushPromises();
  expect(f.w.text()).not.toContain("已保存");
  expect(f.w.find("input").exists()).toBe(false);
  f.w.unmount();
});
