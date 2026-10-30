import { it, expect } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import TransferListTable from "../src/components/TransferListTable.vue";
import TransferListFilters from "../src/components/TransferListFilters.vue";
import KitStatusTag from "../src/components/KitStatusTag.vue";
import KitPayMethod from "../src/components/KitPayMethod.vue";
import KitEmpty from "../src/components/KitEmpty.vue";
import KitFilterBar from "../src/components/KitFilterBar.vue";
import KitField from "../src/components/KitField.vue";
import KitSelect from "../src/components/KitSelect.vue";
import KitModal from "../src/components/KitModal.vue";
import { freshTransferFilters } from "../src/transfer-filters";
import { transferListFixture } from "./fixtures/transfer-list";
import RedpackPage from "../src/components/RedpackPage.vue";
import TransferBatchPage from "../src/components/TransferBatchPage.vue";
import { nextTick } from "vue";

// Minimal ApiClient stub: redpacks list is empty, caps allow writes.
const stubClient = (area: "admin" | "merchant") =>
  ({
    area,
    request: async (path: string) => {
      if (path.startsWith("/redpacks")) return { items: [], open: true };
      if (path.startsWith("/transfer-batches"))
        return { items: [], total: 0, next_before_id: "0" };
      if (path.startsWith("/transfers/capabilities"))
        return { writes_enabled: true, channels: [] };
      return {};
    },
    requestResult: async () => ({ status: 200, data: {} }),
  }) as unknown as import("../src/api").ApiClient;

// Wave 2: the list operates like Pro and looks like the kit.

for (const area of ["admin", "merchant"] as const) {
  it(`${area} list renders Pro's columns in order`, () => {
    const view = mount(TransferListTable, {
      props: { items: [transferListFixture()], area, busy: false },
    });
    const headers = view.findAll("thead th").map((h) => h.text());
    const expected =
      area === "admin"
        ? [
            "交易号第三方交易号",
            "商户号",
            "付款方式备注",
            "付款账号姓名",
            "付款金额花费金额",
            "手续费",
            "提交时间付款时间",
            "状态",
            "操作",
          ]
        : [
            "交易号第三方交易号",
            "付款方式备注",
            "付款账号姓名",
            "付款金额花费金额",
            "手续费",
            "提交时间付款时间",
            "状态",
            "操作",
          ];
    expect(headers).toEqual(expected);
    view.unmount();
  });

  it(`${area} list shows states via KitStatusTag and method via KitPayMethod`, () => {
    const view = mount(TransferListTable, {
      props: {
        items: [transferListFixture({ status: "succeeded", type: "wxpay" })],
        area,
        busy: false,
      },
    });
    const tag = view.getComponent(KitStatusTag);
    expect(tag.text()).toBe("转账成功");
    expect(tag.find(".pa-tag--success").exists()).toBe(true);
    const method = view.getComponent(KitPayMethod);
    expect(method.text()).toContain("微信");
    expect(method.find("img.pa-method-icon--wxpay").exists()).toBe(true);
    view.unmount();
  });

  it(`${area} list shows KitEmpty when there are no records`, () => {
    const view = mount(TransferListTable, {
      props: { items: [], area, busy: false },
    });
    expect(view.findComponent(KitEmpty).exists()).toBe(true);
    expect(view.findComponent(KitEmpty).text()).toContain("暂无数据");
    view.unmount();
  });

  it(`${area} keeps Pro's row actions (付款详情 / 查询状态) and adds 获取凭证`, () => {
    const view = mount(TransferListTable, {
      props: {
        items: [transferListFixture({ status: "unknown" })],
        area,
        busy: false,
      },
    });
    const actions = view
      .findAll("td.transfer-actions button")
      .map((b) => b.text());
    expect(actions).toContain("付款详情");
    expect(actions).toContain("查询状态");
    expect(actions).toContain("获取凭证");
    view.unmount();
  });

  it(`${area} shows 手续费 ¥ and the 备注 under 付款方式 from the row`, () => {
    const view = mount(TransferListTable, {
      props: {
        items: [
          transferListFixture({
            fee_amount: "150",
            cost_amount: "9007199254741143",
            remark: "转账业务备注",
          }),
        ],
        area,
        busy: false,
      },
    });
    const text = view.find("tbody tr").text();
    expect(text).toContain("¥1.50");
    expect(view.find(".transfer-remark").text()).toBe("转账业务备注");
    view.unmount();
  });

  it(`${area} 获取凭证 is enabled for an alipay transfer and emits proof`, async () => {
    const view = mount(TransferListTable, {
      props: {
        items: [transferListFixture({ status: "succeeded", type: "alipay" })],
        area,
        busy: false,
      },
    });
    const btn = view
      .findAll("td.transfer-actions button")
      .find((b) => b.text() === "获取凭证")!;
    expect(btn.attributes("disabled")).toBeUndefined();
    await btn.trigger("click");
    expect(view.emitted("proof")).toBeTruthy();
    view.unmount();
  });

  it(`${area} 获取凭证 is disabled with a reason for a wxpay transfer`, () => {
    const view = mount(TransferListTable, {
      props: {
        items: [transferListFixture({ status: "succeeded", type: "wxpay" })],
        area,
        busy: false,
      },
    });
    const btn = view
      .findAll("td.transfer-actions button")
      .find((b) => b.text() === "获取凭证")!;
    expect(btn.attributes("disabled")).toBeDefined();
    expect(btn.attributes("title")).toContain("仅支付宝付款支持电子回单");
    view.unmount();
  });
}

it("filters use KitFilterBar with Pro's fields and the extra (展开) status/channel/date fields", async () => {
  const view = mount(TransferListFilters, {
    props: {
      modelValue: freshTransferFilters(),
      area: "admin",
      loading: false,
    },
  });
  expect(view.findComponent(KitFilterBar).exists()).toBe(true);
  // Expand the 展开 section so the extra fields mount.
  await view.find(".pa-filter-toggle").trigger("click");
  const labels = view.findAllComponents(KitField).map((f) => f.props("label"));
  // Primary fields (always shown): 搜索字段 + 搜索内容 + 付款方式 + 商户号.
  expect(labels.slice(0, 4)).toEqual([
    "搜索字段",
    "搜索内容",
    "付款方式",
    "商户号",
  ]);
  // Extra (展开) fields.
  for (const extra of ["状态", "通道", "开始日期", "结束日期"])
    expect(labels).toContain(extra);
  // At least two KitSelect (搜索字段 / 付款方式 / 状态) — native <select> is gone.
  expect(view.findAllComponents(KitSelect).length).toBeGreaterThanOrEqual(2);
  expect(view.find("select").exists()).toBe(false);
  view.unmount();
});

it("filters emit query (only when valid) and reset through KitFilterBar", async () => {
  const view = mount(TransferListFilters, {
    props: {
      modelValue: freshTransferFilters(),
      area: "merchant",
      loading: false,
    },
  });
  await view.find("form.pa-filter").trigger("submit");
  await view.find("form.pa-filter").trigger("reset");
  expect(view.emitted("query")).toBeTruthy();
  expect(view.emitted("reset")).toBeTruthy();
  view.unmount();
});

for (const area of ["admin", "merchant"] as const) {
  it(`${area} redpack page uses kit (创建红包 → KitModal, KitEmpty, KitFilterless list)`, async () => {
    const view = mount(RedpackPage, { props: { client: stubClient(area) } });
    await flushPromises();
    // 创建红包 primary button exists; no modal until clicked.
    const createBtn = view
      .findAll("button")
      .find((b) => b.text().includes("创建红包"));
    expect(createBtn).toBeTruthy();
    expect(view.findComponent(KitModal).exists()).toBe(false);
    expect(view.findComponent(KitEmpty).exists()).toBe(true);
    expect(view.find("select").exists()).toBe(false);
    await createBtn!.trigger("click");
    await nextTick();
    const modal = view.findComponent(KitModal);
    expect(modal.exists()).toBe(true);
    const labels = modal
      .findAllComponents(KitField)
      .map((f) => f.props("label"));
    // Pro-style red dialog fields (GOEPAY semantics): type + biz_no + money + remark.
    for (const l of ["通道选择", "交易号", "红包金额", "红包备注"])
      expect(labels).toContain(l);
    if (area === "admin") expect(labels).toContain("付款商户");
    view.unmount();
  });

  it(`${area} batch page uses KitFilterBar + KitEmpty, no native select`, async () => {
    const view = mount(TransferBatchPage, {
      props: { client: stubClient(area) },
    });
    await flushPromises();
    expect(view.findComponent(KitFilterBar).exists()).toBe(true);
    expect(view.find("select").exists()).toBe(false);
    const hasNew = view
      .findAll("button")
      .some((b) => b.text().includes("新建批量付款"));
    expect(hasNew).toBe(true);
    // merchant loads on mount → empty list → KitEmpty; admin needs a merchant id first.
    if (area === "merchant")
      expect(view.findComponent(KitEmpty).exists()).toBe(true);
    view.unmount();
  });
}

it("付款方式 filter validates GOEPAY's rails and binds type into the list query", async () => {
  const { transferFiltersQuery, transferFiltersError, freshTransferFilters } =
    await import("../src/transfer-filters");
  const base = freshTransferFilters();
  // 记账本调拨 is Pro-only alipayfund — not a GOEPAY type, so it is rejected.
  expect(transferFiltersError({ ...base, type: "fundbook" })).toBe(
    "付款方式无效",
  );
  for (const t of ["alipay", "wxpay", "qqpay", "bank"]) {
    expect(transferFiltersError({ ...base, type: t })).toBe("");
    const q = transferFiltersQuery({ ...base, type: t }, 1, 20);
    expect(q.get("type")).toBe(t);
  }
  // Empty type (全部方式) sends no type param.
  expect(transferFiltersQuery(base, 1, 20).get("type")).toBe(null);
});
