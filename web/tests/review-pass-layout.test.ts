import { describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import { ledgerTerm } from "../src/pro-console-fields";
import AddonConfigGroups from "../src/components/AddonConfigGroups.vue";
import ResourcePage from "../src/components/ResourcePage.vue";

describe("ledger display names", () => {
  it("shows Chinese names for known codes and keeps unknown text", () => {
    expect(ledgerTerm("kind", "available")).toBe("可用余额");
    expect(ledgerTerm("description", "payment net credit")).toBe(
      "订单收入入账",
    );
    expect(ledgerTerm("description", "deposit release approved")).toBe(
      "保证金释放（已通过）",
    );
    expect(ledgerTerm("description", "legacy refund failed")).toBe(
      "退款（失败）",
    );
    expect(ledgerTerm("description", "something new")).toBe("something new");
    expect(ledgerTerm("description", "deposit release maybe")).toBe(
      "deposit release maybe",
    );
  });
});

describe("扩展配置 sections", () => {
  it("splits extensions into enabled, disabled and not-installed sections", async () => {
    const w = mount(AddonConfigGroups, {
      props: {
        items: [
          { id: "1", name: "甲", enabled: true },
          { id: "2", name: "乙", enabled: false },
          { id: "3", name: "丙", enabled: false },
        ],
        fields: [
          { name: "name", label: "名称" },
          { name: "enabled", label: "启用资料", type: "boolean" },
        ],
        entityLabel: "扩展参数",
        canCreate: true,
        canUpdate: true,
        canDelete: false,
        busy: false,
        loading: false,
      },
    });
    const sections = w.findAll("section");
    expect(sections.map((s) => s.find("h2").text())).toEqual([
      "已启用的扩展",
      "未启用的扩展",
      "未安装的扩展",
    ]);
    expect(sections.map((s) => s.find(".badge").text())).toEqual([
      "1",
      "2",
      "0",
    ]);
    expect(sections[0].findAll("tbody tr")).toHaveLength(1);
    expect(sections[1].findAll("tbody tr")).toHaveLength(2);
    expect(sections[2].text()).toContain("暂无未安装的扩展");
    await sections[0]
      .findAll("button")
      .find((b) => b.text() === "新增扩展参数")!
      .trigger("click");
    expect(w.emitted("create")).toHaveLength(1);
    await sections[1]
      .findAll("button")
      .find((b) => b.text() === "编辑")!
      .trigger("click");
    expect(w.emitted("open")?.[0]).toEqual([
      "edit",
      { id: "2", name: "乙", enabled: false },
    ]);
  });
});

describe("list page layout", () => {
  it("puts filters in their own card and the add button in the table toolbar with the original summary", async () => {
    const client = new ApiClient("admin");
    vi.spyOn(client, "request").mockImplementation(
      async (path) =>
        (path.endsWith("/schema")
          ? {
              title: "支付方式",
              fields: [
                { name: "name", label: "展示名称", type: "string" },
                { name: "enabled", label: "启用资料", type: "boolean" },
              ],
              permissions: { create: true, update: true, delete: true },
            }
          : {
              items: [
                { id: "1", name: "支付宝", enabled: true },
                { id: "2", name: "微信支付", enabled: false },
              ],
              total: 2,
            }) as never,
    );
    const w = mount(ResourcePage, {
      props: { client, resource: "pay-types" },
    });
    await flushPromises();
    expect(w.find("form.list-filter-card").exists()).toBe(true);
    const tools = w.get("section.records .table-tools");
    expect(tools.text()).toContain("新增支付方式");
    expect(tools.text()).toContain("系统共有2个支付方式");
    expect(tools.get(".table-tools-end").text()).toBe("刷新");
    expect(
      w
        .findAll("tbody .badge")
        .map((b) => [b.text(), b.attributes("data-state")]),
    ).toEqual([
      ["正常", "on"],
      ["已禁用", "bad"],
    ]);
    w.unmount();
  });
});
