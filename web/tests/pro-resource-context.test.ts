// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ResourcePage from "../src/components/ResourcePage.vue";

it("clears old merchant context and rejects a late masked detail after account changes", async () => {
  const old = new ApiClient("admin"),
    current = new ApiClient("admin");
  const schema = {
    title: "商户管理",
    fields: [{ name: "name", label: "名称", type: "string" }],
    permissions: { create: false, update: false, delete: false },
  };
  let finish!: (value: unknown) => void;
  const calls = vi.spyOn(old, "request").mockImplementation(async (path) =>
    path.endsWith("/schema")
      ? schema
      : path.endsWith("/1")
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : { items: [{ id: "1", name: "旧账号商户" }], total: 1 },
  );
  vi.spyOn(current, "request").mockImplementation(
    async (path) =>
      (path.endsWith("/schema")
        ? schema
        : { items: [{ id: "2", name: "新账号商户" }], total: 1 }) as never,
  );
  const w = mount(ResourcePage, {
    props: { client: old, resource: "merchants" },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "更多")!
    .trigger("click");
  await flushPromises();
  (
    Array.from(document.querySelectorAll('[role="menuitem"]')).find(
      (el) => el.textContent?.trim() === "详情",
    ) as HTMLElement
  ).click();
  const signal = calls.mock.calls.find(([path]) => path.endsWith("/1"))?.[1]
    ?.signal;
  await w.setProps({ client: current });
  await flushPromises();
  expect(w.text()).not.toContain("旧账号商户");
  expect(w.text()).toContain("新账号商户");
  expect(signal?.aborted).toBe(true);
  finish({ id: "1", name: "旧账号敏感资料" });
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).not.toContain("旧账号敏感资料");
  w.unmount();
});
