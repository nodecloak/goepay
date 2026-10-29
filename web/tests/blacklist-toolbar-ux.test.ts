import { flushPromises, mount } from "@vue/test-utils";
import { expect, it, vi, describe } from "vitest";
import { ApiClient } from "../src/api";
import ResourcePage from "../src/components/ResourcePage.vue";

// Generic ResourcePage toolbar driven by the schema: a per-field filter select,
// the 导出 CSV button for an exportable resource, and 批量删除 (N) with a confirm
// that posts the selected ids to the resource's reversible batch-delete.
const blacklistSchema = {
  title: "黑名单管理",
  fields: [
    { name: "kind", label: "黑名单类型", type: "select" },
    { name: "value", label: "黑名单内容", type: "string" },
    { name: "enabled", label: "启用", type: "boolean" },
  ],
  permissions: { create: true, update: true, delete: true },
  exportable: true,
  batch_delete: true,
  filters: [
    {
      param: "kind",
      label: "黑名单类型",
      options: [
        { label: "全部类型", value: "" },
        { label: "IP", value: "ip" },
        { label: "域名", value: "domain" },
        { label: "买家", value: "buyer" },
      ],
    },
  ],
};
const rows = [
  { id: "1", kind: "ip", value: "10.0.0.1", enabled: true },
  { id: "2", kind: "ip", value: "10.0.0.2", enabled: true },
];

function makeClient(calls: { path: string; opts?: unknown }[]) {
  const client = new ApiClient("admin");
  vi.spyOn(client, "request").mockImplementation(
    async (path: string, opts?: unknown) => {
      calls.push({ path, opts });
      if (path.endsWith("/schema")) return blacklistSchema as never;
      if (path.endsWith("/batch-delete")) return { deleted: 2 } as never;
      return { items: rows, total: 2 } as never;
    },
  );
  return client;
}

describe("blacklist toolbar (generic ResourcePage)", () => {
  it("renders the kind filter, 导出 button and 批量删除 with selection + confirm", async () => {
    const calls: { path: string; opts?: unknown }[] = [];
    const client = makeClient(calls);
    const w = mount(ResourcePage, {
      props: { client, resource: "blacklist" },
    });
    await flushPromises();

    // Filter: the 黑名单类型 kit select is present.
    expect(w.find('.pa-select-trigger[aria-label="黑名单类型"]').exists()).toBe(
      true,
    );

    // Export: an exportable resource shows a CSV export button.
    expect(w.find(".csv-export").exists()).toBe(true);

    // Batch delete: a select-all + per-row checkboxes, button disabled until a
    // row is picked.
    const selectAll = w.find('th.select-col input[type="checkbox"]');
    expect(selectAll.exists()).toBe(true);
    const batchBtn = w
      .findAll("button")
      .find((b) => b.text().startsWith("批量删除"))!;
    expect(batchBtn.attributes("disabled")).toBeDefined();

    // Pick both rows, confirm, and verify the reversible batch-delete POST.
    await selectAll.setValue(true);
    expect(batchBtn.text()).toContain("(2)");
    await batchBtn.trigger("click");
    await flushPromises();
    // KitModal teleports to body; its submit button carries submit-text 删除.
    const confirm = Array.from(
      document.body.querySelectorAll<HTMLButtonElement>(".pa-modal button"),
    ).find((b) => b.textContent?.trim() === "删除")!;
    expect(confirm).toBeTruthy();
    confirm.click();
    await flushPromises();
    const del = calls.find((c) => c.path.endsWith("/batch-delete"))!;
    expect(del).toBeTruthy();
    expect((del.opts as { method: string }).method).toBe("POST");
    expect((del.opts as { body: { ids: string[] } }).body.ids).toEqual([
      "1",
      "2",
    ]);
    w.unmount();
  });

  it("applies the kind filter to the list query", async () => {
    const calls: { path: string; opts?: unknown }[] = [];
    const client = makeClient(calls);
    const w = mount(ResourcePage, {
      props: { client, resource: "blacklist" },
    });
    await flushPromises();
    await w
      .find('.pa-select-trigger[aria-label="黑名单类型"]')
      .trigger("click");
    const option = w
      .findAll('[role="option"]')
      .find((o) => o.text() === "域名")!;
    await option.trigger("click");
    await flushPromises();
    const listCall = calls
      .filter((c) => !c.path.endsWith("/schema") && c.path.includes("?"))
      .at(-1)!;
    expect(listCall.path).toContain("kind=domain");
    w.unmount();
  });
});
