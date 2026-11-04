import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import ParentChannelPage from "../src/components/ParentChannelPage.vue";
import { ApiClient } from "../src/api";

let wrapper: VueWrapper | undefined;
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
});
const fields = [
  {
    name: "appid",
    namespace: "config",
    secret: false,
    pem: false,
    present: true,
    value: "original-app",
  },
  {
    name: "appkey",
    namespace: "config",
    secret: true,
    pem: false,
    present: true,
  },
  {
    name: "appkey",
    namespace: "certificates",
    secret: true,
    pem: true,
    present: true,
  },
];
const row = {
  id: "9007199254740993",
  name: "模拟通道",
  pay_type: "wxpay",
  plugin: "wxpay",
  configured: true,
  source_channel_id: "8",
  revision: "1",
  account_version: "1",
  enabled: false,
  archived: false,
  mode: 0,
  rate: "99.50",
  fee_rate_bps: 50,
  fingerprint: "a".repeat(64),
  account: { plugin: "wxpay", app_types: ["1"], mode: 0, fields },
  created_at: "2026-10-26T00:00:00Z",
  updated_at: "2026-10-26T00:00:00Z",
};
async function setup() {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-editor244";
  const writes: any[] = [];
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    if (options?.method === "PATCH") {
      writes.push(options.body);
      return row;
    }
    return path.includes("/templates")
      ? { items: [], total: 0 }
      : path.includes("?")
        ? { items: [row], total: 1, page: 1, size: 20 }
        : row;
  });
  wrapper = mount(ParentChannelPage, { props: { client, allowed: true } });
  await flushPromises();
  await wrapper.get("[data-parent-parameters]").trigger("click");
  await flushPromises();
  return { w: wrapper, writes };
}
it("keeps untouched saved credentials and exact namespaces without sending a mask", async () => {
  const { w, writes } = await setup();
  expect(writes).toHaveLength(0);
  expect(w.findAll('input[aria-label="appkey 内容"]')).toHaveLength(1);
  expect(
    (w.get('input[aria-label="appid 内容"]').element as HTMLInputElement).value,
  ).toBe("original-app");
  await w.get('input[aria-label="appid 内容"]').setValue("updated-app");
  await w.get("form[data-parent-editor]").trigger("submit");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({
    expected_fingerprint: row.fingerprint,
    account: {
      config: {
        appid: { operation: "replace", value: "updated-app" },
        appkey: { operation: "keep" },
      },
      certificates: { appkey: { operation: "keep" } },
    },
  });
});
it("submits one explicit secret clear and the separate PEM replacement", async () => {
  const { w, writes } = await setup();
  const params = w.findAll(".parent-param");
  await params[1]!.get('button[aria-label="清除appkey"]').trigger("click");
  await params[2]!
    .get("textarea")
    .setValue(
      "-----BEGIN PRIVATE KEY-----\nsynthetic\n-----END PRIVATE KEY-----",
    );
  expect(writes).toHaveLength(0);
  await w.get("form[data-parent-editor]").trigger("submit");
  await flushPromises();
  expect(writes[0].account.config.appkey).toEqual({ operation: "clear" });
  expect(writes[0].account.certificates.appkey).toEqual({
    operation: "replace",
    value: "-----BEGIN PRIVATE KEY-----\nsynthetic\n-----END PRIVATE KEY-----",
  });
});
it("undoes clear and cancels drafts without writing", async () => {
  const { w, writes } = await setup();
  const secret = w.findAll(".parent-param")[1]!;
  await secret.get("input").setValue("discarded-private-draft");
  await secret.get('button[aria-label="清除appkey"]').trigger("click");
  await secret.get('button[aria-label="撤销清除appkey"]').trigger("click");
  expect(secret.get("input").element.value).toBe("");
  await w
    .get('[role="dialog"]')
    .findAll("button")
    .find((button) => button.text() === "取消")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(writes).toHaveLength(0);
  expect(w.html()).not.toContain("discarded-private-draft");
});
