import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ref } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import ListFilters from "../src/components/ListFilters.vue";
import { freshListFilters } from "../src/list-filters";
import { usePayTypeOptions } from "../src/pay-type-options";
import { ApiClient } from "../src/api";

function fetchResponse(body: unknown, ok = true, status = 200) {
  return {
    ok,
    status,
    json: async () => ({ code: ok ? 0 : -1, data: body }),
    headers: { get: () => null },
  } as unknown as Response;
}

describe("usePayTypeOptions", () => {
  afterEach(() => vi.restoreAllMocks());

  it("maps the endpoint's enabled types + shownames into options", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      fetchResponse({
        items: [
          { value: "alipay", showname: "支付宝" },
          { value: "qqpay", showname: "QQ钱包" },
          { value: "sandbox", showname: "本地模拟" },
        ],
      }),
    );
    const client = ref(new ApiClient("admin"));
    const options = usePayTypeOptions(client);
    // allow the immediate watcher's async load to settle
    await flushPromises();
    expect(options.value).toEqual([
      { value: "alipay", label: "支付宝" },
      { value: "qqpay", label: "QQ钱包" },
      { value: "sandbox", label: "本地模拟" },
    ]);
  });

  it("falls back to the static catalog when the endpoint fails", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("offline"));
    const client = ref(new ApiClient("merchant"));
    const options = usePayTypeOptions(client);
    await flushPromises();
    // fallback is the full catalog superset, not just the old 3
    const values = options.value.map((o) => o.value);
    expect(values).toContain("alipay");
    expect(values).toContain("qqpay");
    expect(values).toContain("ecny");
    expect(values).toContain("sandbox");
  });

  it("drops malformed rows and never yields an empty list", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      fetchResponse({ items: [{ value: "BAD TYPE" }, { nope: 1 }] }),
    );
    const client = ref(new ApiClient("admin"));
    const options = usePayTypeOptions(client);
    await flushPromises();
    // all rows invalid -> fallback catalog, never empty
    expect(options.value.length).toBeGreaterThan(0);
    expect(options.value.map((o) => o.value)).toContain("alipay");
  });
});

describe("ListFilters 支付方式 dropdown", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      fetchResponse({ items: [] }),
    );
  });
  afterEach(() => vi.restoreAllMocks());

  it("renders the data-driven options passed in, with the 全部方式 lead row", async () => {
    const wrapper = mount(ListFilters, {
      props: {
        kind: "orders",
        area: "admin",
        modelValue: freshListFilters(),
        payTypeOptions: [
          { value: "alipay", label: "支付宝" },
          { value: "qqpay", label: "QQ钱包" },
          { value: "bank", label: "银联支付" },
        ],
      },
    });
    await wrapper.get("button.pa-filter-toggle").trigger("click");
    await wrapper
      .get('button.pa-select-trigger[aria-label="支付方式筛选"]')
      .trigger("click");
    const options = wrapper.findAll('[role="option"]').map((x) => x.text());
    expect(options).toContain("全部方式");
    expect(options).toContain("QQ钱包");
    expect(options).toContain("银联支付");
    wrapper.unmount();
  });

  it("falls back to the static three when no options prop is given", async () => {
    const wrapper = mount(ListFilters, {
      props: { kind: "orders", area: "admin", modelValue: freshListFilters() },
    });
    await wrapper.get("button.pa-filter-toggle").trigger("click");
    await wrapper
      .get('button.pa-select-trigger[aria-label="支付方式筛选"]')
      .trigger("click");
    const options = wrapper.findAll('[role="option"]').map((x) => x.text());
    expect(options).toEqual(["全部方式", "支付宝", "微信支付", "本地模拟"]);
    wrapper.unmount();
  });
});
