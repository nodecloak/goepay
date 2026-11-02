import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import {
  businessNoticeSettingsFields,
  parseBusinessNotices,
} from "../src/business-notice";
import BusinessNoticeEvents from "../src/components/BusinessNoticeEvents.vue";
import { settingDefinition } from "../src/settings";

it("uses original UID0 business mode options without changing the risk namespace", () => {
  const fields = [
    { name: "key", label: "配置项", type: "select" as const },
    { name: "value", label: "配置值", type: "string" as const, required: true },
  ];
  const options = businessNoticeSettingsFields(fields, "msgconfig_domain")[1]
    .options;
  expect(options).toEqual([
    { value: "0", label: "关闭提醒" },
    { value: "1", label: "电子邮件" },
    { value: "2", label: "群机器人" },
    { value: "4", label: "Telegram" },
    { value: "5", label: "QQ Bot" },
  ]);
  expect(businessNoticeSettingsFields(fields, "msgconfig_apply")[1].type).toBe(
    "select",
  );
  expect(
    businessNoticeSettingsFields(fields, "msgconfig_regaudit")[1].type,
  ).toBe("select");
  expect(businessNoticeSettingsFields(fields, "msgconfig_risk")).toEqual(
    fields,
  );
});
it("offers SMS only for administrator all-complaint settings and recognizes the channel cap route", () => {
  const fields = [{ name: "value", label: "配置值", type: "string" as const }];
  expect(
    businessNoticeSettingsFields(fields, "msgconfig_complain_all")[0].options,
  ).toContainEqual({ value: "3", label: "短信" });
  const cap = businessNoticeSettingsFields(fields, "msgconfig_channellimit")[0];
  expect(cap.type).toBe("select");
  expect(
    cap.options?.some((o) => (typeof o === "object" ? o.value : o) === "3"),
  ).toBe(false);
  expect(settingDefinition("msgconfig_complain_all").options).toContainEqual({
    value: "3",
    label: "短信",
  });
  expect(settingDefinition("msgconfig_channellimit").options).toContainEqual({
    value: "5",
    label: "QQ Bot",
  });
});
const item = {
  id: "83",
  merchant_id: "1000",
  source_id: "8",
  job_id: "9",
  scene: "domain",
  mode: "1",
  transport: "smtp",
  state: "unknown",
  at: "2026-10-25T10:00:00Z",
};
it("reads administrator supplement history while restricting zero-owner and SMS to their valid scenes", () => {
  const complaint = {
    ...item,
    merchant_id: "0",
    scene: "complain_all",
    mode: "3",
    transport: "sms",
  };
  const cap = { ...item, merchant_id: "0", scene: "channellimit" };
  expect(
    parseBusinessNotices({ items: [complaint, cap], next_cursor: "" }).items,
  ).toEqual([complaint, cap]);
  expect(() =>
    parseBusinessNotices({
      items: [{ ...cap, mode: "3", transport: "sms" }],
      next_cursor: "",
    }),
  ).toThrow();
});
it("validates original history with bounded canonical IDs and does not project unknown private fields", () => {
  const value = parseBusinessNotices({
    items: [{ ...item, secret: "must-not-project" }],
    next_cursor: "83",
  });
  expect(value.items[0]).toEqual(item);
  for (const bad of [
    { ...item, id: "083" },
    { ...item, scene: "other" },
    { ...item, mode: "3" },
    { ...item, state: "sent" },
    { ...item, merchant_id: "0" },
    { ...item, at: "invalid" },
    { ...item, transport: "wechat_template" },
    { ...item, mode: "4", transport: "smtp" },
  ])
    expect(() =>
      parseBusinessNotices({ items: [bad], next_cursor: "" }),
    ).toThrow();
});

it("drops an old administrator response after the original session is revoked", async () => {
  const client = new ApiClient("admin");
  let finish: (value: unknown) => void = () => {};
  vi.spyOn(client, "request").mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }) as never,
  );
  const w = mount(BusinessNoticeEvents, {
    props: { client, ownSession: true, adminClass: "super" },
  });
  await flushPromises();
  await w.setProps({ ownSession: false });
  finish({ items: [item], next_cursor: "" });
  await flushPromises();
  expect(w.text()).not.toContain("1000");
  expect(w.text()).not.toContain("待核实");
  w.unmount();
});
it("loads only authorized read-only history, pages and discards a previous session response", async () => {
  const c = new ApiClient("admin");
  const calls: string[] = [];
  vi.spyOn(c, "request").mockImplementation(async (p, o) => {
    expect(o?.method ?? "GET").toBe("GET");
    calls.push(p);
    return {
      items: [item],
      next_cursor: p.includes("before_id") ? "" : "83",
    } as never;
  });
  const w = mount(BusinessNoticeEvents, {
    props: { client: c, ownSession: true, adminClass: "operator" },
  });
  await flushPromises();
  expect(w.text()).toContain("管理员业务提醒记录");
  expect(w.text()).toContain("待核实");
  await w
    .findAll("button")
    .find((b) => b.text() === "加载更多记录")!
    .trigger("click");
  await flushPromises();
  expect(calls).toEqual([
    "/business-notices?limit=20",
    "/business-notices?limit=20&before_id=83",
  ]);
  expect(w.findAll("button").some((b) => /重发|发送|重试/.test(b.text()))).toBe(
    false,
  );
  await w.setProps({ ownSession: false });
  await flushPromises();
  expect(w.text()).not.toContain("1000");
  expect(calls).toHaveLength(2);
  w.unmount();
});
