import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SystemLogPage from "../src/components/SystemLogPage.vue";
import RuntimeLogPage from "../src/components/RuntimeLogPage.vue";

// A minimal ApiClient stub: SystemLogPage / RuntimeLogPage only call request().
function client() {
  return {
    area: "admin",
    csrfToken: "t",
    request: vi.fn().mockResolvedValue({ items: [], total: 0 }),
  } as unknown as import("../src/api").ApiClient;
}

// Both pages use vue-router composables (useRoute/useRouter); SystemLogPage does.
vi.mock("vue-router", () => ({
  useRoute: () => ({ path: "/admin/system/log", query: {} }),
  useRouter: () => ({ replace: vi.fn().mockResolvedValue(undefined) }),
  RouterLink: { template: "<a><slot/></a>" },
}));

it("system-log filter uses the kit FilterBar (搜索字段 select + 搜索内容) and kit pager", async () => {
  const wrapper = mount(SystemLogPage, { props: { client: client() } });
  await flushPromises();
  // kit FilterBar markup
  expect(wrapper.find(".pa-filter").exists()).toBe(true);
  expect(wrapper.find(".pa-filter-actions").exists()).toBe(true);
  // 搜索字段 kit select trigger + 搜索内容 kit input
  expect(wrapper.find(".pa-select-trigger").exists()).toBe(true);
  expect(wrapper.find(".pa-input").exists()).toBe(true);
  const labels = wrapper.findAll(".pa-label").map((n) => n.text());
  expect(labels).toContain("搜索字段");
  expect(labels).toContain("搜索内容");
  // 查询 / 重置 buttons from the kit bar
  const actions = wrapper.find(".pa-filter-actions").text();
  expect(actions).toContain("查询");
  expect(actions).toContain("重置");
  // kit pager
  expect(wrapper.find(".pa-pager").exists()).toBe(true);
});

it("runtime-log filter uses the kit FilterBar (日期) and kit pager", async () => {
  const wrapper = mount(RuntimeLogPage, {
    props: { client: client(), ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  expect(wrapper.find(".pa-filter").exists()).toBe(true);
  expect(wrapper.find(".pa-label").text()).toContain("日期");
  expect(wrapper.find('input[type="date"].pa-input').exists()).toBe(true);
  expect(wrapper.find(".pa-pager").exists()).toBe(true);
});
