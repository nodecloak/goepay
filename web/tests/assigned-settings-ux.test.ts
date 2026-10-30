import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";
import {
  settingDefinition,
  settingStorage,
  settingsBlocks,
  type SettingDefinition,
} from "../src/settings";

// Each assigned key must resolve to the epay_pro tab, label, control kind and
// (for switches) 开启/关闭 options — so /admin/system/settings presents it the
// way the original settings page does.
it("maps the assigned settings keys to their epay_pro tab, label and control", () => {
  const cases: [string, string, string, SettingDefinition["kind"]][] = [
    ["settle_alipay", "settle", "支付宝结算开关", "select"],
    ["settle_wxpay", "settle", "微信结算开关", "select"],
    ["settle_qqpay", "settle", "QQ钱包结算开关", "select"],
    ["settle_bank", "settle", "银行卡结算开关", "select"],
    ["transfer_wxpay", "transfer", "微信转账接口通道", "select"],
    ["transfer_qqpay", "transfer", "QQ钱包转账接口通道", "select"],
    ["transfer_bank", "transfer", "银行卡转账接口通道", "select"],
    ["user_transfer_batch", "transfer", "用户中心批量代付功能", "select"],
    ["subchannel_open", "merchant", "开启子通道管理功能", "select"],
    ["user_profitsharing", "merchant", "开启添加分账规则(进件)", "select"],
    ["user_totp_open", "merchant", "开启商户TOTP二次验证", "select"],
    ["pay_domain_open", "merchant", "开启授权支付域名添加", "select"],
    ["onecode_domains", "merchant", "聚合收款码可选域名", "textarea"],
    ["blockname", "pay", "商品屏蔽关键词", "text"],
    ["blockalert", "pay", "商品屏蔽显示内容", "text"],
    ["blackmsg", "pay", "黑名单拦截提示内容", "text"],
    ["pageordername", "pay", "扫码页面隐藏商品名称", "select"],
    ["notifyordername", "pay", "回调参数去除商品名称", "select"],
    ["forceqq", "pay", "未填联系QQ禁止支付", "select"],
    ["pay_force_cashier_pids", "pay", "强制跳转收银台商户ID", "text"],
    ["internal_transfer", "merchant", "开启站内转账", "select"],
    ["recharge", "merchant", "开启余额充值", "select"],
    ["onecode", "merchant", "开启聚合收款码", "select"],
    ["complain_open", "complaint", "开启交易投诉", "select"],
    ["applyments_open", "merchant", "开启商户进件功能", "select"],
    ["mchrisk_open", "merchant", "开启商户管控记录查看", "select"],
    ["reg_pay", "merchant", "注册付费", "select"],
    ["reg_pay_price", "merchant", "注册付费金额（元）", "money"],
    ["reg_pay_uid", "merchant", "注册/充值/购买会员收款商户ID", "integer"],
  ];
  for (const [key, tab, label, kind] of cases) {
    const d = settingDefinition(key, key);
    expect(d.tab, key).toBe(tab);
    expect(d.label, key).toBe(label);
    expect(d.kind, key).toBe(kind);
    if (kind === "select") {
      expect(d.options?.map((o) => o.value).sort(), key).toEqual(["0", "1"]);
    }
  }
});

// The switch settings store only 0/1; the pid list and reg values follow the
// original shape (pipe-separated ids, non-negative yuan, positive uid).
it("validates the assigned settings values like the original", () => {
  const store = (key: string, v: string) =>
    settingStorage(settingDefinition(key, key), v);
  expect(store("settle_alipay", "1")).toBe("1");
  expect(store("notifyordername", "0")).toBe("0");
  expect(() => store("forceqq", "2")).toThrow();
  expect(store("blockname", "赌博|色情")).toBe("赌博|色情");
  expect(store("pay_force_cashier_pids", "1001|1002")).toBe("1001|1002");
  expect(store("reg_pay_price", "9.99")).toBe("9.99");
  expect(() => store("reg_pay_price", "1.001")).toThrow();
  expect(store("reg_pay_uid", "5")).toBe("5");
  expect(() => store("reg_pay_uid", "-1")).toThrow();
});

// verifytype is epay_pro's 用户验证方式 (0 邮箱验证, 1 手机验证); the 聚合收款
// domain list takes one origin per line; the feature switches store only 0/1;
// enforce_domains is epay_pro's 未授权支付域名禁止支付.
it("maps verifytype to 邮箱/手机验证 and validates the new merchant keys", () => {
  const d = settingDefinition("verifytype", "verifytype");
  expect(d.tab).toBe("merchant");
  expect(d.label).toBe("用户验证方式");
  expect(d.kind).toBe("select");
  expect(d.options).toEqual([
    { value: "0", label: "邮箱验证" },
    { value: "1", label: "手机验证" },
  ]);
  expect(settingDefinition("enforce_domains", "enforce_domains").label).toBe(
    "未授权支付域名禁止支付",
  );
  expect(settingDefinition("pay_domain_forbid", "pay_domain_forbid").tab).toBe(
    "advanced",
  );
  const store = (key: string, v: string) =>
    settingStorage(settingDefinition(key, key), v);
  expect(store("user_transfer_batch", "1")).toBe("1");
  expect(store("subchannel_open", "0")).toBe("0");
  expect(() => store("user_totp_open", "2")).toThrow();
  expect(() => store("verifytype", "2")).toThrow();
  expect(
    store(
      "onecode_domains",
      " https://pay.example.com\n\nhttps://pay2.example.com/ ",
    ),
  ).toBe("https://pay.example.com\nhttps://pay2.example.com/");
  for (const bad of [
    "a.com\nb.com",
    "http://pay.example.com",
    "https://pay.example.com/collect",
    "https://u:p@pay.example.com",
    Array.from({ length: 21 }, (_, i) => `https://p${i}.example.com`).join(
      "\n",
    ),
  ])
    expect(() => store("onecode_domains", bad), bad).toThrow();
});

// The layout places the new fields under the right tab in epay_pro's order and
// renders the group headings above them.
it("orders the assigned fields under their tab with the original group headings", () => {
  const defsFor = (keys: string[]) => keys.map((k) => settingDefinition(k, k));
  const payBlocks = settingsBlocks(
    "pay",
    defsFor([
      "pageordername",
      "notifyordername",
      "blockname",
      "blockalert",
      "blackmsg",
      "forceqq",
    ]),
  );
  const payKeys = payBlocks.map((b) => b.key);
  expect(payKeys).toContain("group:商品名称与屏蔽");
  expect(payKeys.indexOf("pageordername")).toBeGreaterThan(
    payKeys.indexOf("group:商品名称与屏蔽"),
  );
  const settleBlocks = settingsBlocks(
    "settle",
    defsFor(["settle_alipay", "settle_wxpay", "settle_qqpay", "settle_bank"]),
  );
  expect(settleBlocks.map((b) => b.key)).toContain("group:各收款方式结算开关");
});

// The settings page renders the assigned controls with the design kit: on/off
// switches as KitSwitch (pa-switch), the 结算周期-style selects as KitSelect
// (pa-select), text fields as KitInput (pa-input), with one 保存 per tab.
it("renders the assigned settings on the design kit with one 保存 per tab", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "assigned-settings-kit";
  client.request = vi.fn(async (path: string) => {
    if (path === "/resources/settings/schema")
      return {
        title: "设置",
        fields: [
          {
            name: "key",
            type: "select",
            options: [
              "settle_open",
              "settle_type",
              "settle_alipay",
              "settle_wxpay",
              "settle_qqpay",
              "settle_bank",
            ],
          },
        ],
        permissions: { create: true, update: true, delete: false },
      };
    if (path === "/resources/settings?page=1&limit=100")
      return { items: [], total: 0 };
    throw new Error("unexpected settings API " + path);
  }) as ApiClient["request"];
  const wrapper = mount(SettingsPage, {
    props: { client, adminClass: "super", ownSession: true },
  });
  try {
    await flushPromises();
    // Switch to the 结算规则 tab where the assigned keys live.
    await wrapper.get("#settings-tab-settle").trigger("click");
    const field = (label: string) =>
      wrapper
        .findAll(".setting-field")
        .find((f) => f.find("label").text() === label)!;
    // Per-channel 结算开关 are on/off → KitSwitch.
    for (const label of [
      "支付宝结算开关",
      "微信结算开关",
      "QQ钱包结算开关",
      "银行卡结算开关",
    ]) {
      const f = field(label);
      expect(f, label).toBeTruthy();
      expect(f.find('input[role="switch"]').exists(), label).toBe(true);
      expect(f.find("select").exists(), label).toBe(false);
    }
    // 结算周期 (T+0 / T+1) is a genuine choice → KitSelect, not a switch.
    expect(field("结算周期").find(".pa-select-trigger").exists()).toBe(true);
    expect(field("结算周期").find('input[role="switch"]').exists()).toBe(false);
    // Exactly one 保存 button on the tab.
    const saves = wrapper
      .findAll("button")
      .filter((b) => b.text() === "保存本页设置" || b.text() === "正在保存…");
    expect(saves.length).toBe(1);
  } finally {
    wrapper.unmount();
  }
});
