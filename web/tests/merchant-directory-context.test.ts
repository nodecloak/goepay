// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import { ApiClient } from "../src/api";

it("an older merchant list reply cannot replace a later same-route query", async () => {
  const client = new ApiClient("admin");
  let release: (v: unknown) => void = () => {};
  const pending = new Promise((resolve) => {
    release = resolve;
  });
  vi.spyOn(client, "request").mockImplementation(async (path: string) => {
    if (path.endsWith("/schema"))
      return {
        title: "商户管理",
        fields: [{ name: "name", label: "名称", type: "string" }],
        permissions: { create: false, update: false, delete: false },
      } as any;
    const q = new URL(path, "http://localhost").searchParams;
    if (q.get("q") === "1") return pending as any;
    return { items: [{ id: "2", name: "较后筛选商户" }], total: 1 } as any;
  });
  const wrapper = mount(ResourcePage, {
    props: { client, resource: "merchants", merchantQuery: { uid: "1" } },
  });
  await flushPromises();
  await wrapper.setProps({ merchantQuery: { uid: "2" } });
  await flushPromises();
  expect(wrapper.text()).toContain("较后筛选商户");
  release({ items: [{ id: "1", name: "迟到旧商户" }], total: 1 });
  await flushPromises();
  expect(wrapper.text()).toContain("较后筛选商户");
  expect(wrapper.text()).not.toContain("迟到旧商户");
  wrapper.unmount();
});
