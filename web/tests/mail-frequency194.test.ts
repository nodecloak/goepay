import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import SettingsPage from "../src/components/SettingsPage.vue";
import { ApiClient } from "../src/api";
import { mailValues194, readMailPolicy194 } from "../src/mail-frequency194";
const state = () => ({
  mail_interval_email: "60",
  mail_interval_ip: "60",
  mail_daily_email: "10",
  mail_daily_ip: "20",
  effective: {
    mail_interval_email: "60",
    mail_interval_ip: "0",
    mail_daily_email: "7",
    mail_daily_ip: "11",
  },
  configured: false,
  revision: "0",
  fingerprint: "a".repeat(64),
});
async function setup() {
  const c = new ApiClient("admin");
  c.csrfToken = "mail194-session";
  const writes: any[] = [];
  const request = vi.fn(async (path: string, options?: any): Promise<any> => {
    if (path === "/resources/settings/schema")
      return { fields: [], permissions: { create: true, update: true } };
    if (path.startsWith("/resources/settings?")) return { items: [], total: 0 };
    if (path === "/settings/mail-frequency") {
      if (options?.method === "PATCH") {
        writes.push(options.body);
        return {
          ...options.body,
          effective: { ...options.body },
          configured: true,
          revision: "1",
          fingerprint: "b".repeat(64),
        };
      }
      return state();
    }
    throw new Error("unavailable independent setting");
  });
  c.request = request as ApiClient["request"];
  const w = mount(SettingsPage, {
    props: { client: c, adminClass: "super", ownSession: true },
  });
  await flushPromises();
  await w.get("#settings-tab-mail").trigger("click");
  await flushPromises();
  await w
    .findAll('button[role="tab"]')
    .find((b) => b.text() === "发送频率配置")!
    .trigger("click");
  await flushPromises();
  return { c, w, request, writes };
}
it("original mail frequency fields use a protected four-field save and display the actual legacy limits", async () => {
  const { w, writes } = await setup();
  expect(w.text()).toContain("发送频率配置");
  expect(w.text()).toContain("当前实际生效");
  expect(w.text()).toContain("每日7次");
  for (const [label, value] of [
    ["同一邮箱发送间隔(秒)", "60"],
    ["同一IP发送间隔(秒)", "60"],
    ["同一邮箱每日发送次数", "10"],
    ["同一IP每日发送次数", "20"],
  ]) {
    expect(
      (w.get(`input[aria-label="${label}"]`).element as HTMLInputElement).value,
    ).toBe(value);
  }
  await w.get('input[aria-label="同一IP每日发送次数"]').setValue("18");
  await w.get('form[aria-label="邮件发送频率配置"]').trigger("submit");
  await flushPromises();
  expect(w.text(), w.text()).toContain("保存成功");
  expect(writes).toEqual([
    {
      mail_interval_email: "60",
      mail_interval_ip: "60",
      mail_daily_email: "10",
      mail_daily_ip: "18",
      expected_fingerprint: "a".repeat(64),
    },
  ]);
  expect(w.text()).toContain("保存成功");
  w.unmount();
});
it("replacing the same client's session cannot send an old frequency draft", async () => {
  const { w, c, writes } = await setup();
  await w.get('input[aria-label="同一IP每日发送次数"]').setValue("19");
  c.csrfToken = "different-session";
  await w.get('form[aria-label="邮件发送频率配置"]').trigger("submit");
  await flushPromises();
  expect(writes).toEqual([]);
  expect(w.text()).toContain("重新读取");
  expect(w.find('input[aria-label="同一IP每日发送次数"]').exists()).toBe(false);
  w.unmount();
});

it("rejects missing, zero, noncanonical, excessive or inconsistent frequency responses", () => {
  for (const raw of [0, "0", "01", "1.2", "-1", "86401", "1e2", undefined]) {
    expect(() =>
      mailValues194({ ...state(), mail_interval_email: raw }),
    ).toThrow();
  }
  expect(() => readMailPolicy194({ ...state(), configured: true })).toThrow();
  expect(() =>
    readMailPolicy194({ ...state(), fingerprint: "wrong" }),
  ).toThrow();
  expect(() =>
    readMailPolicy194({ ...state(), revision: "1", configured: true }),
  ).toThrow();
  expect(readMailPolicy194(state()).effective.mail_interval_ip).toBe("0");
});

it("an uncertain save discards its draft and requires an explicit fresh read without a duplicate write", async () => {
  const { w, request, writes } = await setup();
  request.mockImplementation(async (path, options) => {
    if (path === "/settings/mail-frequency" && options?.method === "PATCH") {
      writes.push(options.body);
      throw new Error("synthetic reply lost");
    }
    if (path === "/settings/mail-frequency") return state();
    throw new Error("unexpected request");
  });
  await w.get('form[aria-label="邮件发送频率配置"]').trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(w.text()).toContain("保存结果无法确认，请重新读取后核对");
  expect(w.find('form[aria-label="邮件发送频率配置"]').exists()).toBe(false);
  await w
    .findAll("button")
    .find((b) => b.text() === "重新读取邮件频率配置")!
    .trigger("click");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(w.find('form[aria-label="邮件发送频率配置"]').exists()).toBe(true);
  w.unmount();
});

it("losing own-session authority removes the frequency draft before any save", async () => {
  const { w, writes } = await setup();
  await w.setProps({ ownSession: false });
  await flushPromises();
  expect(w.find('form[aria-label="邮件发送频率配置"]').exists()).toBe(false);
  expect(writes).toEqual([]);
  w.unmount();
});
