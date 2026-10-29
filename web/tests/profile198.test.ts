// @vitest-environment jsdom
import { it, expect, vi, afterEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import AccountPage from "../src/components/AccountPage.vue";
import { ApiClient } from "../src/api";
const mounted: Array<{ unmount(): void }> = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
});
async function setup(known = true) {
  const client = new ApiClient("merchant");
  client.csrfToken = "profile198-session";
  const writes: any[] = [];
  client.request = vi.fn(async (path: string, options?: any): Promise<any> => {
    if (path === "/account/profile") {
      if (options?.method === "PATCH")
        writes.push(JSON.parse(JSON.stringify(options.body)));
      return {
        name: "本地商户",
        email: "",
        phone: "13800138000",
        pay: "1",
        uid: "9007199254740993",
        addtime: "2020-01-02 11:04:05",
        registration_time_known: known,
      };
    }
    if (path === "/account/email")
      return {
        email: "",
        verified: false,
        email_enabled: false,
        requires: "password",
        fingerprint: "a".repeat(64),
        email_interval: 60,
      };
    return {};
  }) as ApiClient["request"];
  const w = mount(AccountPage, { props: { client, section: "profile" } });
  mounted.push(w);
  await flushPromises();
  return { w, writes };
}
it("shows the original read-only UID and registration fields without rounding an ID or sending them in profile edits", async () => {
  const { w, writes } = await setup();
  expect(w.text()).toContain("商户ID");
  expect(w.text()).toContain("9007199254740993");
  expect(w.text()).toContain("注册时间");
  expect(w.text()).toContain("2020-01-02 11:04:05");
  expect(w.find('input[name="uid"]').exists()).toBe(false);
  await w.findAll("form")[0]!.trigger("submit");
  await flushPromises();
  expect(writes).toEqual([
    { name: "本地商户", email: "", phone: "13800138000" },
  ]);
});
it("reads back the editInfo contact phone and sends an edited value in the profile save", async () => {
  const { w, writes } = await setup();
  const phone = w.get<HTMLInputElement>('input[autocomplete="tel"]');
  expect(phone.element.value).toBe("13800138000");
  await phone.setValue("13900139000");
  await w.findAll("form")[0]!.trigger("submit");
  await flushPromises();
  expect(writes).toEqual([
    { name: "本地商户", email: "", phone: "13900139000" },
  ]);
});
it("does not present an import clock as registration time when the source explicitly marks it unknown", async () => {
  const { w } = await setup(false);
  expect(w.text()).toContain("原注册时间未提供");
  expect(w.text()).not.toContain("2020-01-02 11:04:05");
  expect(w.text()).toContain("9007199254740993");
});
it("clears the previous account profile before a replacement session fails to load", async () => {
  const { w } = await setup();
  const replacement = new ApiClient("merchant");
  replacement.csrfToken = "different-profile198-session";
  replacement.request = vi.fn(async () => {
    throw Error("synthetic account source unavailable");
  });
  await w.setProps({ client: replacement });
  await flushPromises();
  expect(
    w.get<HTMLInputElement>('input[autocomplete="organization"]').element.value,
  ).toBe("");
  expect(w.text()).not.toContain("9007199254740993");
  expect(w.text()).not.toContain("2020-01-02 11:04:05");
});
