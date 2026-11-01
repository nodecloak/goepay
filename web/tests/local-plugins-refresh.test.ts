import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import LocalPluginsPage from "../src/components/LocalPluginsPage.vue";
import { loadPluginCatalog, type CatalogEntry } from "../src/plugin-catalog";
vi.mock("../src/plugin-catalog", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../src/plugin-catalog")>()),
  loadPluginCatalog: vi.fn(),
}));
afterEach(() => vi.resetAllMocks());
it("recovers a failed catalog read from the page and prevents duplicate refreshes", async () => {
  let resolve!: (v: CatalogEntry[]) => void;
  const retry = new Promise<CatalogEntry[]>((r) => (resolve = r));
  vi.mocked(loadPluginCatalog)
    .mockRejectedValueOnce(new Error("read failed"))
    .mockReturnValueOnce(retry);
  const view = mount(LocalPluginsPage);
  try {
    await flushPromises();
    expect(view.get('[role="alert"]').text()).toContain("读取失败");
    const refresh = view
      .findAll("button")
      .find((b) => b.text() === "刷新本地插件列表");
    expect(refresh).toBeDefined();
    await refresh!.trigger("click");
    await refresh!.trigger("click");
    expect(loadPluginCatalog).toHaveBeenCalledTimes(2);
    expect(refresh!.attributes("disabled")).toBeDefined();
    resolve([
      {
        name: "alipay",
        title: "支付宝",
        desc: "Synthetic payment plugin",
        type: "payment",
        price: "0",
        author: "GOEPAY",
        version: "1",
        pay: ["alipay"],
        trans: [],
      },
    ]);
    await flushPromises();
    expect(view.find("tbody").text()).toContain("支付宝");
    expect(view.find('[role="alert"]').exists()).toBe(false);
    expect(refresh!.attributes("disabled")).toBeUndefined();
  } finally {
    view.unmount();
  }
});
