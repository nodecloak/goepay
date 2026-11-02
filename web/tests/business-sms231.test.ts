// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import BusinessSMSSettings231 from "../src/components/BusinessSMSSettings231.vue";
function state(revision = "1") {
  const templates: Record<string, string> = {};
  for (const p of ["aliyun", "qcloud", "smsbao"])
    for (const s of ["balance", "complain", "group"])
      templates[p + "_" + s] = "";
  return { revision, templates, active_provider: "aliyun", ready: {} };
}
const modal = {
  props: ["title", "loading"],
  emits: ["submit", "close"],
  template: `<div><slot/><button class="confirm" @click="$emit('submit')">确认保存</button><button class="cancel" @click="$emit('close')">取消</button></div>`,
};
it("saves all nine independent templates with original revision and no test sends", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "local231";
  const writes: any[] = [];
  const request = vi.spyOn(c, "request").mockImplementation(async (p, o) => {
    expect(p).toBe("/settings/business-sms");
    if (o?.method === "PUT") {
      writes.push(o.body);
      return { ...state("2"), templates: (o.body as any).templates } as never;
    }
    return state() as never;
  });
  const w = mount(BusinessSMSSettings231, {
    props: { client: c, adminClass: "super", ownSession: true },
    global: { stubs: { KitModal: modal } },
  });
  await flushPromises();
  expect(w.findAll("input")).toHaveLength(9);
  await w
    .get('input[aria-label="阿里云 余额不足模板"]')
    .setValue("SMS_BALANCE");
  await w.get("button.primary").trigger("click");
  await w.get(".cancel").trigger("click");
  expect(writes).toHaveLength(0);
  await w.get("button.primary").trigger("click");
  await w.get(".confirm").trigger("click");
  await flushPromises();
  expect(writes).toHaveLength(1);
  expect(writes[0].expected_revision).toBe("1");
  expect(Object.keys(writes[0].templates)).toHaveLength(9);
  expect(writes[0].templates.aliyun_balance).toBe("SMS_BALANCE");
  expect(w.text()).toContain("未发送短信");
  expect(request).toHaveBeenCalledTimes(2);
  w.unmount();
});
it("unknown save outcome keeps explicit recovery and never automatically retries", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "local231";
  let sends = 0;
  vi.spyOn(c, "request").mockImplementation(async (_p, o) => {
    if (o?.method === "PUT") {
      sends++;
      throw Error("connection lost");
    }
    return state() as never;
  });
  const w = mount(BusinessSMSSettings231, {
    props: { client: c, adminClass: "super", ownSession: true },
    global: { stubs: { KitModal: modal } },
  });
  await flushPromises();
  await w
    .get('input[aria-label="阿里云 余额不足模板"]')
    .setValue("SMS_BALANCE");
  await w.get("button.primary").trigger("click");
  await w.get(".confirm").trigger("click");
  await flushPromises();
  expect(sends).toBe(1);
  expect(w.text()).toContain("保存结果无法确认");
  expect(w.get("button.primary").attributes("disabled")).toBeDefined();
  w.unmount();
});
