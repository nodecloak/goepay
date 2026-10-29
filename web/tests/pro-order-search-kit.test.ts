import { describe, it, expect } from "vitest";
import { mount } from "@vue/test-utils";
import { computed, defineComponent, h, ref } from "vue";
import KitField from "../src/components/KitField.vue";
import KitSelect from "../src/components/KitSelect.vue";
import KitInput from "../src/components/KitInput.vue";
import { orderSearchFields, refundSearchFields } from "../src/list-filters";

// Mirrors the App.vue search-group: Pro's 搜索字段 (KitSelect) + 搜索内容 (KitInput).
const SearchGroup = defineComponent({
  components: { KitField, KitSelect, KitInput },
  props: {
    kind: { type: String, required: true },
    // App.vue's searchFieldOptions does NOT branch on area, so a merchant gets
    // the same field list as an admin. The prop exists only to prove that.
    area: { type: String, default: "admin" },
  },
  setup(props) {
    const searchField = ref("");
    const keyword = ref("");
    const options = computed(() => [
      { value: "", label: "综合搜索" },
      ...(props.kind === "refunds"
        ? refundSearchFields
        : orderSearchFields
      ).map((f) => ({ value: f.value, label: f.label })),
    ]);
    const inputmode = computed<"decimal" | "text">(() =>
      ["money", "realmoney", "getmoney"].includes(searchField.value)
        ? "decimal"
        : "text",
    );
    return () =>
      h("div", { class: "search-group" }, [
        h(KitField, { label: "搜索字段" }, () =>
          h(KitSelect, {
            modelValue: searchField.value,
            options: options.value,
            placeholder: "综合搜索",
            "onUpdate:modelValue": (v: string) => (searchField.value = v),
          }),
        ),
        h(KitField, { label: "搜索内容" }, () =>
          h(KitInput, {
            modelValue: keyword.value,
            inputmode: inputmode.value,
            "onUpdate:modelValue": (v: string) => (keyword.value = v),
          }),
        ),
      ]);
  },
});

describe("Pro order search-field on the design kit", () => {
  it("renders 搜索字段 + 搜索内容 labels via KitField", () => {
    const w = mount(SearchGroup, { props: { kind: "orders" } });
    const labels = w.findAll(".pa-label").map((x) => x.text());
    expect(labels).toEqual(["搜索字段", "搜索内容"]);
    // kit controls, not native select/input
    expect(w.find(".pa-select-trigger").exists()).toBe(true);
    expect(w.find(".pa-input input").exists()).toBe(true);
    expect(w.find("select").exists()).toBe(false);
    w.unmount();
  });

  it("offers Pro's exact order search fields in order", async () => {
    const w = mount(SearchGroup, { props: { kind: "orders" } });
    await w.find(".pa-select-trigger").trigger("click");
    const items = w.findAll('.pa-menu [role="option"]').map((x) => x.text());
    expect(items).toEqual([
      "综合搜索",
      "系统订单号",
      "商户订单号",
      "接口订单号",
      "商品名称",
      "订单金额（元）",
      "实付金额（元）",
      "分成金额（元）",
      "支付账号",
      "支付 IP",
      "网站域名",
      "扩展参数",
    ]);
    w.unmount();
  });

  it("offers Pro's exact refund search fields", async () => {
    const w = mount(SearchGroup, { props: { kind: "refunds" } });
    await w.find(".pa-select-trigger").trigger("click");
    const items = w.findAll('.pa-menu [role="option"]').map((x) => x.text());
    expect(items).toEqual([
      "综合搜索",
      "退款单号",
      "商户退款单号",
      "关联订单号",
    ]);
    w.unmount();
  });

  it("switches 搜索内容 to decimal inputmode for money fields", async () => {
    const w = mount(SearchGroup, { props: { kind: "orders" } });
    await w.find(".pa-select-trigger").trigger("click");
    const money = w
      .findAll('.pa-menu [role="option"]')
      .find((x) => x.text() === "订单金额（元）");
    await money!.trigger("click");
    expect(w.find(".pa-input input").attributes("inputmode")).toBe("decimal");
    w.unmount();
  });

  it("gives the MERCHANT the same full field list as the admin (not just 综合搜索)", async () => {
    const read = async (area: string) => {
      const w = mount(SearchGroup, { props: { kind: "orders", area } });
      // the trigger shows only the current value (综合搜索); the list is in the popover
      expect(w.find(".pa-select-trigger").text()).toContain("综合搜索");
      await w.find(".pa-select-trigger").trigger("click");
      const items = w.findAll('.pa-menu [role="option"]').map((x) => x.text());
      w.unmount();
      return items;
    };
    const admin = await read("admin");
    const merchant = await read("merchant");
    expect(merchant).toEqual(admin);
    expect(merchant).toContain("系统订单号");
    expect(merchant).toContain("扩展参数");
    expect(merchant.length).toBeGreaterThan(1); // not collapsed to 综合搜索 only
  });
});
