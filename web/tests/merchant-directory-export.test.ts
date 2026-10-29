// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import CsvExportButton from "../src/components/CsvExportButton.vue";
import { ApiClient } from "../src/api";

function stub(client: ApiClient) {
  vi.spyOn(client, "request").mockImplementation(async (path: string) => {
    if (path.endsWith("/schema"))
      return {
        title: "商户管理",
        fields: [{ name: "name", label: "名称", type: "string" }],
        permissions: { create: true, update: false, delete: false },
      } as any;
    return { items: [{ id: "1000", name: "示例商户" }], total: 1 } as any;
  });
}

it("admin merchant list offers a CSV export carrying the directory filters (Pro user/export)", async () => {
  const client = new ApiClient("admin");
  stub(client);
  const wrapper = mount(ResourcePage, {
    props: {
      client,
      resource: "merchants",
      merchantQuery: { uid: "1000", dstatus: "pay_1", order: "money_desc" },
    },
  });
  await flushPromises();
  const button = wrapper.findComponent(CsvExportButton);
  expect(button.exists()).toBe(true);
  const path = button.props("path") as string;
  const q = new URL(path, "http://localhost");
  expect(q.pathname).toBe("/resources/merchants");
  expect(q.searchParams.get("format")).toBe("csv");
  // the active list filters ride along, so the export matches the view
  expect(q.searchParams.get("dstatus")).toBe("pay_1");
  expect(q.searchParams.get("order")).toBe("money_desc");
  expect(button.props("filename")).toBe("商户列表.csv");
  wrapper.unmount();
});

it("the CSV export is admin-only — a merchant principal sees no export", async () => {
  const client = new ApiClient("merchant");
  stub(client);
  const wrapper = mount(ResourcePage, {
    props: { client, resource: "merchants" },
  });
  await flushPromises();
  expect(wrapper.findComponent(CsvExportButton).exists()).toBe(false);
  wrapper.unmount();
});
