import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import MerchantNoticeSettings from "../src/components/MerchantNoticeSettings.vue";
import { resolvePage, viewKind } from "../src/menu";
const settings = {
  id: "7",
  merchant_id: "1000",
  revision: "2",
  voice_order: false,
  email_masked: "o***@example.test",
  msgconfig: {
    order: 2,
    settle: 0,
    login: 0,
    complain: 0,
    mchrisk: 0,
    balance: 0,
    group: 0,
    domain_audit: 0,
    daily_report: 4,
    order_money: "10.00",
    balance_money: "20.00",
  },
  configured: { qqbot_appsecret: true },
  target_masks: { qqbot_appsecret: "********" },
};
afterEach(() => vi.restoreAllMocks());
it("exposes separate owner and managed merchant notification editors", () => {
  expect(resolvePage("merchant", "/account/notice")?.title).toBe(
    "消息提醒设置",
  );
  expect(viewKind("merchant", "/account/notice")).toBe("merchant-notice");
  expect(resolvePage("admin", "/merchant/notice")?.title).toBe("商户消息提醒");
  expect(viewKind("admin", "/merchant/notice")).toBe("merchant-notice");
});
it("keeps original config fields and submits private replacement once with its original version", async () => {
  const c = new ApiClient("merchant");
  let saved: unknown;
  vi.spyOn(c, "request").mockImplementation(async (_p, o) => {
    if (o?.method === "PUT") {
      saved = o.body;
      return { ...settings, id: "8", revision: "3" } as never;
    }
    return settings as never;
  });
  const w = mount(MerchantNoticeSettings, {
    props: { client: c, ownSession: true },
  });
  await flushPromises();
  expect(w.text()).toContain("订单金额提醒门槛");
  expect(w.text()).toContain("Telegram");
  await w.get('[data-target-action="qqbot_appsecret"]').setValue("replace");
  await w
    .get('[data-target-value="qqbot_appsecret"]')
    .setValue("synthetic-ui-secret80");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(saved).toMatchObject({
    expected_profile_id: "7",
    msgconfig: { order: 2, order_money: "10.00", balance_money: "20.00" },
    targets: { qqbot_appsecret: "synthetic-ui-secret80" },
    clear_fields: [],
  });
  expect(w.find('[data-target-value="qqbot_appsecret"]').exists()).toBe(false);
  expect(w.text()).not.toContain("synthetic-ui-secret80");
  expect(w.text()).toContain("设置已保存");
  w.unmount();
});
it("clears secrets after an uncertain save and requires a fresh version before another edit", async () => {
  const c = new ApiClient("merchant");
  let sends = 0;
  vi.spyOn(c, "request").mockImplementation(async (_p, o) => {
    if (o?.method === "PUT") {
      sends++;
      throw Error("版本冲突");
    }
    return settings as never;
  });
  const w = mount(MerchantNoticeSettings, {
    props: { client: c, ownSession: true },
  });
  await flushPromises();
  await w.get('[data-target-action="msgrobot_url"]').setValue("replace");
  await w
    .get('[data-target-value="msgrobot_url"]')
    .setValue(
      "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic80",
    );
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(w.find("form").exists()).toBe(false);
  expect(w.text()).toContain("版本冲突");
  expect(w.text()).not.toContain("key=synthetic80");
  expect(sends).toBe(1);
  w.unmount();
});
it("never loads configuration through a delegated session", async () => {
  const c = new ApiClient("merchant"),
    read = vi.spyOn(c, "request");
  const w = mount(MerchantNoticeSettings, {
    props: { client: c, ownSession: false },
  });
  await flushPromises();
  expect(read).not.toHaveBeenCalled();
  expect(w.find("form").exists()).toBe(false);
  w.unmount();
});
it("cannot reuse one merchant's pending settings response in another merchant's editor", async () => {
  const c = new ApiClient("admin");
  let resolveFirst: (v: unknown) => void = () => {};
  vi.spyOn(c, "request").mockImplementation(async (p) =>
    p.includes("1000")
      ? ((await new Promise<unknown>((resolve) => {
          resolveFirst = resolve;
        })) as never)
      : ({
          ...settings,
          id: "9",
          merchant_id: "1001",
          msgconfig: { ...settings.msgconfig, order_money: "99.00" },
        } as never),
  );
  const w = mount(MerchantNoticeSettings, {
    props: {
      client: c,
      ownSession: true,
      adminClass: "operator",
      merchantID: "1000",
    },
  });
  await flushPromises();
  await w.setProps({ merchantID: "1001" });
  await flushPromises();
  resolveFirst(settings);
  await flushPromises();
  expect(
    (w.get('[name="order_money"]').element as HTMLInputElement).value,
  ).toBe("99.00");
  w.unmount();
});
