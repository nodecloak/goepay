// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import SettlementListFilters from "../src/components/SettlementListFilters.vue";
import { freshSettlementFilters } from "../src/settlement-filters";

// The settle list filter must match epay_pro settle/list: 搜索内容 + 结算方式
// (admin also 商户号) as primary fields, status/date behind 展开, with 重置/查询.
describe("settle list filter (kit, Pro model)", () => {
  function mountBar(area: "admin" | "merchant") {
    return mount(SettlementListFilters, {
      props: { modelValue: freshSettlementFilters(), area, loading: false },
    });
  }

  it("renders Pro's primary field labels and 重置/查询 through the kit bar", () => {
    const w = mountBar("admin");
    expect(w.find(".pa-filter").exists()).toBe(true);
    const labels = w.findAll(".pa-label").map((l) => l.text());
    expect(labels).toContain("搜索内容");
    expect(labels).toContain("商户号");
    expect(labels).toContain("结算方式");
    const buttons = w.findAll("button").map((b) => b.text());
    expect(buttons).toContain("重置");
    expect(buttons).toContain("查询");
    expect(buttons).toContain("展开");
  });

  it("hides 商户号 for the merchant area", () => {
    const w = mountBar("merchant");
    expect(w.findAll(".pa-label").map((l) => l.text())).not.toContain("商户号");
  });

  it("reveals status and date fields only after 展开", async () => {
    const w = mountBar("admin");
    expect(w.findAll(".pa-label").map((l) => l.text())).not.toContain("状态");
    await w
      .findAll("button")
      .find((b) => b.text() === "展开")!
      .trigger("click");
    const labels = w.findAll(".pa-label").map((l) => l.text());
    expect(labels).toContain("状态");
    expect(labels).toContain("开始日期");
    expect(labels).toContain("结束日期");
  });

  it("emits submit on 查询 and reset on 重置", async () => {
    const w = mountBar("admin");
    await w.find("form").trigger("submit");
    await w.find("form").trigger("reset");
    expect(w.emitted("submit")).toBeTruthy();
    expect(w.emitted("reset")).toBeTruthy();
  });
});
