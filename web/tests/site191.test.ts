import { afterEach, expect, it, vi } from "vitest";
import { defineComponent } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { readPublicSite191, usePublicSite191 } from "../src/site191";
import { settingDefinition, settingStorage } from "../src/settings";

const data = {
  sitename: "网站191",
  title: "标题191",
  keywords: "",
  description: "",
  orgname: "",
  email: "",
  kfqq: "",
  kfqq_url: "",
  wework_url: "",
  qqqun: "",
  login_subtitle: "",
  homepage: "0",
  homepage_url: "",
};
afterEach(() => {
  vi.unstubAllGlobals();
});
it("keeps only public original keys and does not turn settings into markup", () => {
  const parsed = readPublicSite191({
    ...data,
    sitename: "网站<b>",
    captcha_key: "private191",
  });
  expect(parsed.sitename).toBe("网站<b>");
  expect(JSON.stringify(parsed)).not.toContain("private191");
});
it.each([
  { homepage: "02" },
  { sitename: "a".repeat(65) },
  { title: "a\nb" },
  { kfqq_url: "javascript:alert(191)" },
  { homepage_url: "https://u:p@example.invalid" },
  { homepage: "2", homepage_url: "" },
  { orgname: "a".repeat(1001) },
])("rejects invalid public settings %j", (change) => {
  expect(() => readPublicSite191({ ...data, ...change })).toThrow();
});
it("retains the original homepage choices in website settings", () => {
  const field = settingDefinition("homepage");
  expect(field.tab).toBe("site");
  expect(field.options?.map((x) => x.value)).toEqual(["0", "1", "2", "3"]);
  expect(settingDefinition("sitename").label).toBe("网站名称");
});
it("can clear optional original website fields while retaining required names and mode", () => {
  for (const key of [
    "title",
    "keywords",
    "description",
    "orgname",
    "email",
    "kfqq",
    "kfqq_url",
    "wework_url",
    "qqqun",
    "login_subtitle",
    "homepage_url",
  ])
    expect(settingStorage(settingDefinition(key), "")).toBe("");
  for (const key of ["sitename", "site_name", "homepage"])
    expect(() => settingStorage(settingDefinition(key), "")).toThrow();
});
it("a late older read cannot replace the branding refreshed after save", async () => {
  let resolveFirst!: (value: unknown) => void;
  const fetcher = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveFirst = resolve;
        }),
    )
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        code: 0,
        data: { ...data, sitename: "已保存的网站191" },
      }),
    });
  vi.stubGlobal("fetch", fetcher);
  const component = defineComponent({
    setup: usePublicSite191,
    template: "<p>{{site?.sitename}}</p>",
  });
  const wrapper = mount(component);
  await wrapper.vm.reload();
  resolveFirst({ ok: true, json: async () => ({ code: 0, data }) });
  await flushPromises();
  expect(wrapper.text()).toBe("已保存的网站191");
  expect(fetcher.mock.calls[0]?.[1].signal.aborted).toBe(true);
  wrapper.unmount();
});
