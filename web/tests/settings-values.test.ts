import { it, expect } from "vitest";
import {
  settingDefinition,
  settingDisplay,
  settingStorage,
  settingsBlocks,
} from "../src/settings";
it("keeps the implemented deposit controls in merchant settings with typed units", () => {
  const open = settingDefinition("user_deposit");
  const minimum = settingDefinition("user_deposit_min");
  const days = settingDefinition("user_deposit_day");
  expect(open.tab).toBe("merchant");
  expect(open.options).toEqual([
    { value: "1", label: "开启" },
    { value: "0", label: "关闭" },
  ]);
  expect(minimum.tab).toBe("merchant");
  expect(minimum.kind).toBe("money");
  expect(settingStorage(minimum, "12.34")).toBe("12.34");
  expect(() => settingStorage(minimum, "0.001")).toThrow();
  expect(days.kind).toBe("integer");
  expect(() => settingStorage(days, "1.5")).toThrow();
});
it("groups notice channels, events and WeChat templates as distinct settings blocks", () => {
  const blocks = settingsBlocks(
    "notice",
    ["msgconfig_regaudit", "msgconfig_order", "wxnotice_tpl_order"].map((key) =>
      settingDefinition(key),
    ),
  );
  const headings = blocks.filter((b) => b.type === "group").map((b) => b.title);
  expect(headings).toContain("管理员接收消息设置");
  expect(headings).toContain("用户接收消息类型开关");
  expect(headings).toContain("微信公众号消息提醒设置");
});
it("keeps notification failure controls with payment settings and validates their real range", () => {
  for (const key of ["auto_check_notify", "check_notify_notice"]) {
    const d = settingDefinition(key);
    expect(d.tab).toBe("pay");
    expect(d.kind).toBe("select");
    expect(settingStorage(d, "0")).toBe("0");
    expect(() => settingStorage(d, "2")).toThrow();
  }
  const count = settingDefinition("check_notify_count");
  expect(count.tab).toBe("pay");
  expect(count.kind).toBe("integer");
  expect(settingStorage(count, "1000")).toBe("1000");
  expect(() => settingStorage(count, "1001")).toThrow();
});
it("places the original ordinary transfer permission with payment controls and explains inheritance", () => {
  const d = settingDefinition("user_transfer");
  expect(d.tab).toBe("transfer");
  expect(d.label).toContain("代付功能");
  expect(d.hint).toContain("商户自身权限");
  expect(settingStorage(d, "1")).toBe("1");
});
it("keeps original payment amount settings in yuan with exact limits", () => {
  for (const key of ["pay_minmoney", "pay_maxmoney"]) {
    const d = settingDefinition(key);
    expect(d.tab).toBe("pay");
    expect(d.kind).toBe("money");
    expect(settingStorage(d, "90071992547409.93")).toBe("90071992547409.93");
    expect(settingStorage(d, "0")).toBe("0.00");
    for (const invalid of ["-1", "1e2", "0.001", "92233720368547758.08"])
      expect(() => settingStorage(d, invalid)).toThrow();
  }
});
it("offers the original four global settlement modes in the settlement tab", () => {
  const d = settingDefinition("settle_open");
  expect(d.tab).toBe("settle");
  expect(d.kind).toBe("select");
  expect(d.options).toEqual([
    { value: "0", label: "关闭结算" },
    { value: "1", label: "只开启每日自动结算" },
    { value: "2", label: "只开启手动申请结算" },
    { value: "3", label: "开启自动+手动结算" },
  ]);
  for (const value of ["0", "1", "2", "3"])
    expect(settingStorage(d, value)).toBe(value);
  expect(() => settingStorage(d, "4")).toThrow();
});
it("keeps original admin risk and pending-business notification mode namespaces distinct", () => {
  const risk = settingDefinition("msgconfig_risk"),
    business = settingDefinition("msgconfig_domain");
  expect(risk.options?.find((x) => x.value === "2")?.label).toBe("Telegram");
  expect(business.options?.find((x) => x.value === "2")?.label).toBe(
    "群机器人",
  );
  expect(settingStorage(business, "4")).toBe("4");
  expect(() => settingStorage(risk, "4")).toThrow("有效选项");
});
it("uses exact integer conversion for money and basis points and rejects fractional cents", () => {
  const minimum = settingDefinition("settlement_minimum_cents"),
    rate = settingDefinition("settlement_fee_rate_bps");
  expect(settingDisplay(minimum, "9007199254740993")).toBe("90071992547409.93");
  expect(settingStorage(minimum, "1.23")).toBe("123");
  expect(settingDisplay(rate, "101")).toBe("1.01");
  expect(settingStorage(rate, "1.01")).toBe("101");
  for (const invalid of ["0.005", "-1", "1e2", "01.00", "Infinity"])
    expect(() => settingStorage(minimum, invalid)).toThrow();
  expect(() => settingStorage(rate, "100.01")).toThrow("上限");
  expect(() => settingStorage(minimum, "")).toThrow("明确");
});

it("clears only optional values whose consumers support a default", () => {
  for (const key of [
    "blockname",
    "blockalert",
    "blackmsg",
    "pay_force_cashier_pids",
    "user_deposit_min",
    "user_deposit_day",
    "wxnotice_tpl_order_no",
  ])
    expect(settingStorage(settingDefinition(key), "")).toBe("");
  expect(settingDefinition("user_deposit_min").label).toBe(
    "平台最低缴纳保证金（元）",
  );
  expect(settingDefinition("user_deposit_min").hint).toContain("禁止新支付");
  for (const key of ["user_deposit", "wxnotice_tpl_order"])
    expect(() => settingStorage(settingDefinition(key), "")).toThrow();
});
