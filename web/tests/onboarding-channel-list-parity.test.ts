import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ResourcePage from "../src/components/ResourcePage.vue";

afterEach(() => vi.restoreAllMocks());
it("keeps onboarding form JSON in the editor and detail rather than the default list", async () => {
  const client = new ApiClient("admin");
  const item = {
    id: "7",
    name: "测试渠道",
    provider: "alipay",
    form_schema: { fields: [{ name: "contact", type: "string" }] },
    enabled: false,
  };
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    if (path.endsWith("/schema"))
      return {
        title: "进件渠道配置",
        fields: [
          { name: "name", label: "渠道名称", type: "string" },
          {
            name: "provider",
            label: "服务商",
            type: "select",
            options: [{ value: "alipay", label: "支付宝" }],
          },
          { name: "form_schema", label: "资料表单 JSON", type: "json" },
          { name: "enabled", label: "启用资料", type: "boolean" },
        ],
        permissions: { create: true, update: true, delete: true },
      } as never;
    if (path.endsWith("/7")) return item as never;
    return { items: [item], total: 1 } as never;
  });
  const view = mount(ResourcePage, {
    props: { client, resource: "onboarding-channels" },
  });
  try {
    await flushPromises();
    expect(view.find("table").text()).not.toContain("资料表单 JSON");
    expect(view.find("tbody").text()).not.toContain('"fields"');
    await view
      .findAll("tbody button")
      .find((b) => b.text() === "编辑")!
      .trigger("click");
    await flushPromises();
    expect(view.get("#field-form_schema").element).toHaveProperty(
      "value",
      JSON.stringify(item.form_schema, null, 2),
    );
  } finally {
    view.unmount();
  }
});
