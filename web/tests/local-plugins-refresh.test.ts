import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import LocalPluginsPage from "../src/components/LocalPluginsPage.vue";
import { loadPluginCatalog, type CatalogEntry } from "../src/plugin-catalog";
vi.mock("../src/plugin-catalog", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../src/plugin-catalog")>()),
  loadPluginCatalog: vi.fn(),
}));
afterEach(() => vi.resetAllMocks());
it("reads the catalog again when the page is reopened after a failure", async () => {
  let resolve!: (v: CatalogEntry[]) => void;
  const retry = new Promise<CatalogEntry[]>((r) => (resolve = r));
  vi.mocked(loadPluginCatalog)
    .mockRejectedValueOnce(new Error("read failed"))
    .mockReturnValueOnce(retry);
  let view = mount(LocalPluginsPage);
  try {
    await flushPromises();
    expect(view.get('[role="alert"]').text()).toContain("读取失败");
    expect(
      view.findAll("button").some((b) => b.text() === "刷新本地插件列表"),
    ).toBe(false);
    view.unmount();
    view = mount(LocalPluginsPage);
    expect(loadPluginCatalog).toHaveBeenCalledTimes(2);
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
  } finally {
    view.unmount();
  }
});
