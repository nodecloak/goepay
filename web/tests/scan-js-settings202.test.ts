// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ScanJSSettings202 from "../src/components/ScanJSSettings202.vue";

function settings202(available = false) {
  return {
    enabled: false,
    configured: false,
    revision: "0",
    fingerprint: "a".repeat(64),
    profile_id: "",
    profile_revision: "",
    can_update: true,
    available,
    profiles: [
      { id: "202", revision: "7", channel_id: "8", app_id: "wx-native202" },
    ],
  };
}
it("native scan settings need current application selection and explicit confirmation before saving", async () => {
  const calls: { path: string; options: any }[] = [];
  const client = new ApiClient("admin");
  client.csrfToken = "settings202-csrf";
  client.request = (async (path: string, options: any = {}) => {
    calls.push({ path, options });
    if (options.method === "PATCH")
      return {
        ...settings202(true),
        enabled: true,
        configured: true,
        revision: "1",
        fingerprint: "b".repeat(64),
        profile_id: "202",
        profile_revision: "7",
      };
    return settings202();
  }) as typeof client.request;
  const w = mount(ScanJSSettings202, {
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  await w.get('input[type="checkbox"]').setValue(true);
  const save = () =>
    w.findAll("button").find((b) => b.text() === "保存原生扫码设置")!;
  expect(save().attributes("disabled")).toBeDefined();
  await w.get("select").setValue("202");
  await save().trigger("click");
  expect(calls.filter((c) => c.options.method === "PATCH")).toHaveLength(0);
  await w
    .findAll("button")
    .find((b) => b.text() === "确认授权原生扫码")!
    .trigger("click");
  await flushPromises();
  expect(
    calls
      .filter((c) => c.options.method === "PATCH")
      .map((c) => ({ path: c.path, body: c.options.body })),
  ).toEqual([
    {
      path: "/settings/scanpay-js",
      body: {
        enabled: true,
        profile_id: "202",
        profile_revision: "7",
        expected_fingerprint: "a".repeat(64),
      },
    },
  ]);
  expect(w.text()).toContain("原生扫码授权已保存");
  w.unmount();
});
it("a rotated native application never silently reselects its replacement", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "settings202-csrf";
  client.request = vi.fn(async () => ({
    ...settings202(),
    enabled: true,
    configured: true,
    revision: "9",
    profile_id: "202",
    profile_revision: "6",
  })) as typeof client.request;
  const w = mount(ScanJSSettings202, {
    props: { client, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  expect((w.get("select").element as HTMLSelectElement).value).toBe("");
  expect(w.text()).toContain("请重新选择当前应用并确认授权");
  expect(
    w
      .findAll("button")
      .find((b) => b.text() === "保存原生扫码设置")!
      .attributes("disabled"),
  ).toBeDefined();
  w.unmount();
});
it("delegated or operator sessions cannot read or save native scan authorization", async () => {
  for (const [adminClass, ownSession] of [
    ["super", false],
    ["operator", true],
  ] as const) {
    const client = new ApiClient("admin");
    client.csrfToken = "settings202-csrf";
    const request = vi.fn();
    client.request = request;
    const w = mount(ScanJSSettings202, {
      props: { client, adminClass, ownSession },
    });
    await flushPromises();
    expect(request).not.toHaveBeenCalled();
    expect(w.find("input").exists()).toBe(false);
    w.unmount();
  }
});
