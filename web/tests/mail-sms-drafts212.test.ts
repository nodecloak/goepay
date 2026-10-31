import { afterEach, expect, it } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { reactive, type Component } from "vue";
import { ApiClient, ApiError } from "../src/api";
import MailSettings195 from "../src/components/MailSettings195.vue";
import MailFrequency194 from "../src/components/MailFrequency194.vue";
import SmsSettings134 from "../src/components/SmsSettings134.vue";
const wrappers: VueWrapper[] = [];
afterEach(() => {
  for (const w of wrappers.splice(0)) w.unmount();
  document.body.innerHTML = "";
});
const smtp = () => ({
  name: "smtp",
  title: "SMTP发信",
  configured: false,
  status: 0,
  config: { host: "", port: "465", username: "" },
  password_set: false,
  revision: "0",
  fingerprint: "a".repeat(64),
  test_available: false,
  effective_source: "deployment",
});
const sms = () => ({
  providers: ["aliyun", "qcloud", "smsbao"].map((provider) => ({
    provider,
    configured: false,
    template: "",
    sign: "",
    principal: "",
  })),
  active: "",
  status: 0,
  fingerprint: "a".repeat(64),
});
function render(
  component: Component,
  request: (path: string, options?: any) => Promise<any>,
  discard?: () => Promise<boolean>,
) {
  const entries = new Map<symbol, () => boolean>();
  const client = reactive(
    Object.assign(new ApiClient("admin"), {
      csrfToken: "mail-sms212",
      request,
    }),
  );
  const w = mount(component, {
    attachTo: document.body,
    props: { client, adminClass: "super", ownSession: true },
    global: {
      stubs: { Teleport: true },
      provide: {
        [Symbol.for("goepay:settings-drafts212")]: {
          register(_label: string, dirty: () => boolean) {
            const key = Symbol();
            entries.set(key, dirty);
            return () => entries.delete(key);
          },
          confirmDiscard: discard,
        },
      },
    },
  });
  wrappers.push(w);
  return {
    w,
    client,
    dirty: () => [...entries.values()].some((dirty) => dirty()),
  };
}
function button(w: VueWrapper, label: string) {
  const b = w.findAll("button").find((b) => b.text() === label);
  expect(b).toBeDefined();
  return b!;
}
async function fillSMTP(w: VueWrapper) {
  await button(w, "账号设置").trigger("click");
  await flushPromises();
  await w
    .get('[aria-label="SMTP服务器"]')
    .setValue("smtp.synthetic212.invalid");
  await w.get('[aria-label="邮箱账号"]').setValue("test212@example.invalid");
  await w.get('[aria-label="邮箱密码"]').setValue("synthetic-private212");
}
it("SMTP suspend preserves actual fields; explicit cancel unregisters the draft state", async () => {
  const { w, dirty } = render(MailSettings195, async () => smtp());
  await flushPromises();
  expect(dirty()).toBe(false);
  await fillSMTP(w);
  expect(dirty()).toBe(true);
  await button(w, "收起并保留草稿").trigger("click");
  expect(dirty()).toBe(true);
  await button(w, "继续编辑草稿").trigger("click");
  await flushPromises();
  expect(
    (w.get('[aria-label="邮箱密码"]').element as HTMLInputElement).value,
  ).toBe("synthetic-private212");
  await button(w, "取消").trigger("click");
  expect(dirty()).toBe(false);
});
it("a successful SMTP save clears the guard and password, while a rejected save stays dirty", async () => {
  for (const reject of [false, true]) {
    const { w, dirty } = render(MailSettings195, async (_path, options) => {
      if (options?.method === "PATCH") {
        if (reject) throw new ApiError("Synthetic rejection212", 400, -1);
        return {
          ...smtp(),
          config: {
            host: "smtp.synthetic212.invalid",
            port: "465",
            username: "test212@example.invalid",
          },
          configured: true,
          password_set: true,
          revision: "1",
          fingerprint: "b".repeat(64),
          effective_source: "console",
        };
      }
      return smtp();
    });
    await flushPromises();
    await fillSMTP(w);
    await w.get("form").trigger("submit");
    await flushPromises();
    expect(dirty()).toBe(reject);
    expect(w.html()).not.toContain('value="synthetic-private212"');
  }
});
it("SMTP local reread cancellation keeps a minimized draft; an approved stale-session confirmation cannot read", async () => {
  let approve!: (value: boolean) => void,
    reads = 0;
  const { w, client, dirty } = render(
    MailSettings195,
    async () => {
      reads++;
      return smtp();
    },
    () =>
      new Promise((resolve) => {
        approve = resolve;
      }),
  );
  await flushPromises();
  await fillSMTP(w);
  await button(w, "收起并保留草稿").trigger("click");
  const before = reads;
  await button(w, "重新读取SMTP账号").trigger("click");
  await flushPromises();
  approve(false);
  await flushPromises();
  expect(dirty()).toBe(true);
  expect(reads).toBe(before);
  await button(w, "重新读取SMTP账号").trigger("click");
  await flushPromises();
  client.csrfToken = "rotated212";
  await flushPromises();
  const afterRotation = reads;
  approve(true);
  await flushPromises();
  expect(reads).toBe(afterRotation);
  expect(dirty()).toBe(false);
});
it("a failed SMTP save followed by a failed reread remains guarded", async () => {
  let fail = false;
  const { w, dirty } = render(
    MailSettings195,
    async (_path, options) => {
      if (options?.method === "PATCH") {
        fail = true;
        throw Error("Lost response212");
      }
      if (fail) throw Error("Offline212");
      return smtp();
    },
    async () => true,
  );
  await flushPromises();
  await fillSMTP(w);
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(dirty()).toBe(true);
  await button(w, "重新读取SMTP账号").trigger("click");
  await flushPromises();
  expect(dirty()).toBe(true);
});
it("SMS credential drafts survive suspend and explicit cancel discards them", async () => {
  const { w, dirty } = render(SmsSettings134, async () => sms());
  await flushPromises();
  await button(w, "服务商设置").trigger("click");
  await flushPromises();
  const inputs = w.findAll(".pa-modal input");
  await inputs[0]!.setValue("LTAI-test212");
  await inputs[1]!.setValue("synthetic-sms-secret212");
  expect(dirty()).toBe(true);
  await button(w, "收起并保留草稿").trigger("click");
  expect(dirty()).toBe(true);
  await button(w, "继续编辑草稿").trigger("click");
  await flushPromises();
  expect(
    (w.findAll(".pa-modal input")[1]!.element as HTMLInputElement).value,
  ).toBe("synthetic-sms-secret212");
  await button(w, "取消").trigger("click");
  expect(dirty()).toBe(false);
});
it("mail frequency draft stays dirty after a rejected write and failed reread", async () => {
  let fail = false;
  const policy = {
    mail_interval_email: "60",
    mail_interval_ip: "60",
    mail_daily_email: "10",
    mail_daily_ip: "20",
    effective: {
      mail_interval_email: "60",
      mail_interval_ip: "60",
      mail_daily_email: "10",
      mail_daily_ip: "20",
    },
    configured: false,
    revision: "0",
    fingerprint: "a".repeat(64),
  };
  const { w, dirty } = render(
    MailFrequency194,
    async (_path, options) => {
      if (options?.method === "PATCH") {
        fail = true;
        throw Error("Lost response212");
      }
      if (fail) throw Error("Offline212");
      return policy;
    },
    async () => true,
  );
  await flushPromises();
  await w.get('input[name="mail_daily_email"]').setValue("11");
  expect(dirty()).toBe(true);
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(dirty()).toBe(true);
  await button(w, "重新读取邮件频率配置").trigger("click");
  await flushPromises();
  expect(dirty()).toBe(true);
});
it("an unresolved SMS test remains guarded after closing and cannot be silently removed by local reread", async () => {
  let reads = 0,
    confirmations = 0;
  const { w, dirty } = render(
    SmsSettings134,
    async (path) => {
      if (path.endsWith("/test")) throw Error("Lost SMS test response212");
      reads++;
      return { ...sms(), active: "aliyun", status: 1 };
    },
    async () => {
      confirmations++;
      return false;
    },
  );
  await flushPromises();
  await button(w, "测试发送").trigger("click");
  await flushPromises();
  await w.get(".pa-modal input").setValue("13800138000");
  await button(w, "发送").trigger("click");
  await flushPromises();
  expect(w.text()).toContain("发送结果无法确认");
  await button(w, "取消").trigger("click");
  expect(dirty()).toBe(true);
  const before = reads;
  await button(w, "重新读取短信网关").trigger("click");
  await flushPromises();
  expect(confirmations).toBe(1);
  expect(reads).toBe(before);
  expect(dirty()).toBe(true);
});
it("same-client session rotation discards SMTP and SMS private drafts before they can be resumed", async () => {
  for (const component of [MailSettings195, SmsSettings134]) {
    const { w, client, dirty } = render(component, async () =>
      component === MailSettings195 ? smtp() : sms(),
    );
    await flushPromises();
    if (component === MailSettings195) await fillSMTP(w);
    else {
      await button(w, "服务商设置").trigger("click");
      await flushPromises();
      await w.get(".pa-modal input").setValue("LTAI-private212");
    }
    expect(dirty()).toBe(true);
    client.csrfToken = "new-subject212";
    await flushPromises();
    expect(dirty()).toBe(false);
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(
      w
        .findAll("input")
        .some((i) =>
          (i.element as HTMLInputElement).value.includes("private212"),
        ),
    ).toBe(false);
  }
});
it("SMTP local reread preserves an independent frequency draft without claiming to discard it", async () => {
  let confirmations = 0;
  const policy = {
    mail_interval_email: "60",
    mail_interval_ip: "60",
    mail_daily_email: "10",
    mail_daily_ip: "20",
    effective: {
      mail_interval_email: "60",
      mail_interval_ip: "60",
      mail_daily_email: "10",
      mail_daily_ip: "20",
    },
    configured: false,
    revision: "0",
    fingerprint: "a".repeat(64),
  };
  const { w, dirty } = render(
    MailSettings195,
    async (path) => (path.endsWith("mail-frequency") ? policy : smtp()),
    async () => {
      confirmations++;
      return false;
    },
  );
  await flushPromises();
  await button(w, "发送频率配置").trigger("click");
  await flushPromises();
  await w.get('input[name="mail_daily_email"]').setValue("11");
  await button(w, "邮箱渠道配置").trigger("click");
  await button(w, "重新读取SMTP账号").trigger("click");
  await flushPromises();
  expect(confirmations).toBe(0);
  expect(dirty()).toBe(true);
  await button(w, "发送频率配置").trigger("click");
  expect(
    (w.get('input[name="mail_daily_email"]').element as HTMLInputElement).value,
  ).toBe("11");
});
