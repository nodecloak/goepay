import { pageReload248 } from "./helpers/page-reload248";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import Bots from "../src/components/NotificationBotSettings231.vue";
import KitSelect from "../src/components/KitSelect.vue";
import CredentialActionControl from "../src/components/CredentialActionControl.vue";
import KitSwitch from "../src/components/KitSwitch.vue";
import KitModal from "../src/components/KitModal.vue";
import { ApiError } from "../src/api";
import { settingsBlocks, settingDefinition } from "../src/settings";
import { settingsDraftRegistry212Key } from "../src/settings-drafts212";

const empty = () => ({
  revision: "0",
  enabled: false,
  app_id_masked: "",
  secret_present: false,
  bound: false,
  recipient_masked: "",
  phone_masked: "",
  token_source: "",
});
const configured = () => ({
  ...empty(),
  revision: "1",
  enabled: true,
  app_id_masked: "12***78",
  secret_present: true,
});
const wrappers: VueWrapper[] = [];
function setup(
  kind: "robot" | "telegram" | "qqbot" = "qqbot",
  initial = empty(),
  extra: Record<string, unknown> = {},
  registry?: object,
) {
  const request = vi.fn().mockResolvedValue(initial);
  const client = { area: "admin", csrfToken: "synthetic-bots231", request };
  let wrapper = mount(Bots, {
    props: {
      client: client as never,
      kind,
      ownSession: true,
      adminClass: "super",
      ...extra,
    },
    global: {
      stubs: { teleport: true },
      provide: registry
        ? { [settingsDraftRegistry212Key as symbol]: registry }
        : {},
    },
  });
  wrappers.push(wrapper);
  return { wrapper, request, client };
}
async function replace(wrapper: VueWrapper) {
  await wrapper
    .findComponent(CredentialActionControl)
    .get("button")
    .trigger("click");
  await flushPromises();
}
async function submit(wrapper: VueWrapper) {
  await wrapper.find("form").trigger("submit");
  await flushPromises();
  await wrapper.findComponent(KitModal).find("form").trigger("submit");
  await flushPromises();
}
function button(wrapper: VueWrapper, text: string) {
  return wrapper.findAll("button").find((b) => b.text() === text)!;
}
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.useRealTimers();
});

describe("administrator notification accounts", () => {
  it("cancels edited credentials, restores binding availability, and keeps enabled-account clearing unavailable", async () => {
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    expect(
      wrapper
        .get('[aria-label="清除管理员QQ机器人配置凭据"]')
        .attributes("disabled"),
    ).toBeDefined();
    await replace(wrapper);
    await wrapper.get('[aria-label="管理员QQ机器人AppID"]').setValue("new123");
    await wrapper
      .get('[aria-label="管理员QQ机器人AppSecret"]')
      .setValue("private-new231");
    expect(button(wrapper, "开始QQ绑定").attributes("disabled")).toBeDefined();
    await wrapper
      .get('[aria-label="取消修改管理员QQ机器人配置凭据"]')
      .trigger("click");
    expect(
      wrapper.get('[aria-label="管理员QQ机器人AppID"]').element,
    ).toHaveProperty("value", "");
    expect(
      wrapper.get('[aria-label="管理员QQ机器人AppSecret"]').element,
    ).toHaveProperty("value", "");
    expect(
      button(wrapper, "开始QQ绑定").attributes("disabled"),
    ).toBeUndefined();
    expect(request).toHaveBeenCalledTimes(1);
  });
  it("places three account editors in the notice layout instead of editable raw bindings", () => {
    const fields = ["msgtelegram_chatid", "qqbot_openid", "mail_recv"].map(
      (k) => settingDefinition(k)!,
    );
    const blocks = settingsBlocks("notice", fields);
    expect(
      blocks
        .filter((b) => b.type === "embed")
        .map((b) => b.type === "embed" && b.embed),
    ).toEqual(
      expect.arrayContaining([
        "notice-robot231",
        "notice-telegram231",
        "notice-qqbot231",
      ]),
    );
    expect(
      blocks.filter((b) => b.type === "field").map((b) => b.key),
    ).not.toContain("qqbot_openid");
    expect(
      blocks.filter((b) => b.type === "field").map((b) => b.key),
    ).not.toContain("msgtelegram_chatid");
  });

  it.each([{ ownSession: false }, { adminClass: "operator" }])(
    "does not load credentials without an original super session %j",
    async (extra) => {
      let { wrapper, request } = setup("qqbot", empty(), extra);
      await flushPromises();
      expect(request).not.toHaveBeenCalled();
      expect(wrapper.find("form").exists()).toBe(false);
    },
  );

  it("shows robot webhook and administrator phone while using masked placeholders", async () => {
    let { wrapper } = setup("robot", {
      ...configured(),
      phone_masked: "138***001",
    });
    await flushPromises();
    expect(
      wrapper
        .find('[aria-label="群机器人Webhook地址"]')
        .attributes("placeholder"),
    ).toBe("12***78");
    expect(
      wrapper.find('[aria-label="管理员手机号码"]').attributes("placeholder"),
    ).toBe("138***001");
    expect(
      (
        wrapper.find('[aria-label="群机器人Webhook地址"]')
          .element as HTMLInputElement
      ).value,
    ).toBe("");
    expect(wrapper.find('[aria-label="管理员手机号码"]').exists()).toBe(true);
  });

  it("saves the complete robot configuration once without starting binding or sending", async () => {
    let { wrapper, request } = setup("robot");
    await flushPromises();
    await replace(wrapper);
    await wrapper
      .find('[aria-label="群机器人Webhook地址"]')
      .setValue(
        "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic231",
      );
    await wrapper.find('[aria-label="管理员手机号码"]').setValue("13800000001");
    wrapper.findComponent(KitSwitch).vm.$emit("update:modelValue", true);
    request.mockResolvedValueOnce(configured());
    await submit(wrapper);
    expect(request).toHaveBeenCalledTimes(2);
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/robot",
      expect.objectContaining({
        method: "PUT",
        body: {
          expected_revision: "0",
          enabled: true,
          credential_action: "replace",
          app_id:
            "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic231",
          secret: "",
          phone: "13800000001",
          token_source: "",
        },
      }),
    );
    expect(wrapper.text()).toContain("已保存");
    expect(
      (
        wrapper.find('[aria-label="群机器人Webhook地址"]')
          .element as HTMLInputElement
      ).value,
    ).toBe("");
  });

  it("uses the OAuth Telegram token without retaining a dedicated secret", async () => {
    let { wrapper, request } = setup("telegram");
    await flushPromises();
    await replace(wrapper);
    await wrapper
      .find('[aria-label="管理员Telegram Chat Id"]')
      .setValue("-100123456789");
    await wrapper
      .find('[aria-label="Telegram Bot Token"]')
      .setValue("synthetic-dedicated231");
    wrapper
      .findAllComponents(KitSelect)[0]!
      .vm.$emit("update:modelValue", "oauth");
    await flushPromises();
    wrapper.findComponent(KitSwitch).vm.$emit("update:modelValue", true);
    request.mockResolvedValueOnce({ ...configured(), token_source: "oauth" });
    await submit(wrapper);
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/telegram",
      expect.objectContaining({
        method: "PUT",
        body: {
          expected_revision: "0",
          enabled: true,
          credential_action: "replace",
          app_id: "-100123456789",
          secret: "",
          phone: "",
          token_source: "oauth",
        },
      }),
    );
    expect(wrapper.html()).not.toContain("synthetic-dedicated231");
  });
  it("saves an administrator SMS phone without requiring a disabled robot webhook", async () => {
    let { wrapper, request } = setup("robot");
    await flushPromises();
    await replace(wrapper);
    await wrapper.find('[aria-label="管理员手机号码"]').setValue("13800000001");
    request.mockResolvedValueOnce({
      ...empty(),
      revision: "1",
      phone_masked: "138***001",
    });
    await submit(wrapper);
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/robot",
      expect.objectContaining({
        method: "PUT",
        body: {
          expected_revision: "0",
          enabled: false,
          credential_action: "replace",
          app_id: "",
          secret: "",
          phone: "13800000001",
          token_source: "",
        },
      }),
    );
    expect(wrapper.text()).toContain("管理员手机已保存");
    expect(
      wrapper.find('[aria-label="群机器人Webhook地址"]').attributes("required"),
    ).toBeUndefined();
  });

  it("rejects incomplete QQ credentials before dispatch and never offers an OpenID input", async () => {
    let { wrapper, request } = setup();
    await flushPromises();
    await replace(wrapper);
    await wrapper
      .find('[aria-label="管理员QQ机器人AppID"]')
      .setValue("12345678");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(wrapper.findComponent(KitModal).exists()).toBe(false);
    expect(request).toHaveBeenCalledTimes(1);
    expect(wrapper.find('input[aria-label*="OpenID"]').exists()).toBe(false);
  });

  it("sends empty credential fields for keep and clears only after disabling", async () => {
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    wrapper.findComponent(KitSwitch).vm.$emit("update:modelValue", false);
    request.mockResolvedValueOnce({
      ...configured(),
      revision: "2",
      enabled: false,
    });
    await submit(wrapper);
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/qqbot",
      expect.objectContaining({
        body: {
          expected_revision: "1",
          enabled: false,
          credential_action: "keep",
          app_id: "",
          secret: "",
          phone: "",
          token_source: "",
        },
      }),
    );
    await wrapper
      .get('[aria-label="清除管理员QQ机器人配置凭据"]')
      .trigger("click");
    request.mockResolvedValueOnce({ ...empty(), revision: "3" });
    await submit(wrapper);
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/qqbot",
      expect.objectContaining({
        body: expect.objectContaining({
          expected_revision: "2",
          credential_action: "clear",
          enabled: false,
        }),
      }),
    );
  });

  it("clears drafts and blocks mutation after an unknown save until explicit readback", async () => {
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    await replace(wrapper);
    await wrapper
      .find('[aria-label="管理员QQ机器人AppID"]')
      .setValue("87654321");
    await wrapper
      .find('[aria-label="管理员QQ机器人AppSecret"]')
      .setValue("synthetic-private231");
    request.mockRejectedValueOnce(new Error("lost response"));
    await submit(wrapper);
    expect(request).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("不会自动重试");
    expect(
      (
        wrapper.find('[aria-label="管理员QQ机器人AppSecret"]')
          .element as HTMLInputElement
      ).value,
    ).toBe("");
    expect(
      button(wrapper, "保存管理员QQ机器人配置").attributes("disabled"),
    ).toBeDefined();
    wrapper = await pageReload248(wrapper);
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(3);
    expect(wrapper.text()).not.toContain("不会自动重试");
  });

  it("starts a deliberate in-memory QQ challenge and removes it when polling observes binding", async () => {
    vi.useFakeTimers();
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    request.mockResolvedValueOnce({
      id: "a".repeat(48),
      challenge: "GOEPAY-0123456789abcdefabcd",
      expires_at: new Date(Date.now() + 300000).toISOString(),
    });
    await button(wrapper, "开始QQ绑定").trigger("click");
    await flushPromises();
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/qqbot/binding",
      expect.objectContaining({
        method: "POST",
        body: { expected_revision: "1" },
      }),
    );
    expect(wrapper.text()).toContain("GOEPAY-0123456789abcdefabcd");
    expect(
      wrapper.find('[aria-label="QQ机器人Webhook URL"]').attributes("readonly"),
    ).toBeDefined();
    expect(
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ).not.toContain("GOEPAY-");
    request.mockResolvedValueOnce({
      ...configured(),
      revision: "2",
      bound: true,
      recipient_masked: "op***id",
    });
    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(wrapper.text()).toContain("已绑定");
    expect(wrapper.text()).not.toContain("GOEPAY-0123456789abcdefabcd");
  });

  it("cancels exactly the active binding window without putting its code in a URL", async () => {
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    request.mockResolvedValueOnce({
      id: "b".repeat(48),
      challenge: "GOEPAY-0123456789abcdefabcd",
      expires_at: new Date(Date.now() + 300000).toISOString(),
    });
    await button(wrapper, "开始QQ绑定").trigger("click");
    await flushPromises();
    request.mockResolvedValueOnce({ cancelled: true });
    await button(wrapper, "取消本次绑定").trigger("click");
    await flushPromises();
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/qqbot/binding/" + "b".repeat(48),
      expect.objectContaining({ method: "DELETE" }),
    );
    expect(wrapper.text()).not.toContain("GOEPAY-0123456789abcdefabcd");
    expect(
      request.mock.calls.some((call) => String(call[0]).includes("GOEPAY-")),
    ).toBe(false);
  });
  it("does not repeat a binding start after its response is lost", async () => {
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    request.mockRejectedValueOnce(new Error("lost binding response"));
    await button(wrapper, "开始QQ绑定").trigger("click");
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("不会自动重试");
    expect(button(wrapper, "开始QQ绑定").attributes("disabled")).toBeDefined();
    expect(wrapper.find("code").exists()).toBe(false);
  });
  it("stops polling after a read failure and requires explicit configuration readback", async () => {
    vi.useFakeTimers();
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    request.mockResolvedValueOnce({
      id: "d".repeat(48),
      challenge: "GOEPAY-0123456789abcdefabcd",
      expires_at: new Date(Date.now() + 300000).toISOString(),
    });
    await button(wrapper, "开始QQ绑定").trigger("click");
    await flushPromises();
    request.mockRejectedValueOnce(new Error("lost read response"));
    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(wrapper.find("code").exists()).toBe(false);
    expect(wrapper.text()).toContain("绑定状态尚未确认");
    await vi.advanceTimersByTimeAsync(10000);
    expect(request).toHaveBeenCalledTimes(3);
  });

  it("stops and hides a binding challenge at its five minute expiry", async () => {
    vi.useFakeTimers();
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    request.mockResolvedValueOnce({
      id: "c".repeat(48),
      challenge: "GOEPAY-0123456789abcdefabcd",
      expires_at: new Date(Date.now() + 300000).toISOString(),
    });
    await button(wrapper, "开始QQ绑定").trigger("click");
    await flushPromises();
    request.mockResolvedValue(configured());
    await vi.advanceTimersByTimeAsync(300000);
    await flushPromises();
    expect(wrapper.text()).not.toContain("GOEPAY-0123456789abcdefabcd");
    expect(wrapper.text()).toContain("已过期");
    const count = request.mock.calls.length;
    await vi.advanceTimersByTimeAsync(6000);
    expect(request).toHaveBeenCalledTimes(count);
  });

  it("requires explicit confirmation before unbinding the saved QQ recipient", async () => {
    let { wrapper, request } = setup("qqbot", {
      ...configured(),
      bound: true,
      recipient_masked: "op***id",
    });
    await flushPromises();
    await button(wrapper, "解除QQ绑定").trigger("click");
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    request.mockResolvedValueOnce({
      ...configured(),
      revision: "2",
      bound: false,
    });
    await wrapper.findComponent(KitModal).find("form").trigger("submit");
    await flushPromises();
    expect(request).toHaveBeenLastCalledWith(
      "/settings/notice-accounts/qqbot/binding",
      expect.objectContaining({
        method: "DELETE",
        body: { expected_revision: "1" },
      }),
    );
    expect(wrapper.text()).toContain("未绑定");
  });

  it("keeps definite validation failures editable but rejects a conflict until readback", async () => {
    let { wrapper, request } = setup("qqbot", configured());
    await flushPromises();
    await replace(wrapper);
    await wrapper
      .find('[aria-label="管理员QQ机器人AppID"]')
      .setValue("87654321");
    await wrapper
      .find('[aria-label="管理员QQ机器人AppSecret"]')
      .setValue("synthetic231");
    request.mockRejectedValueOnce(new ApiError("synthetic invalid", 422, -1));
    await submit(wrapper);
    expect(wrapper.text()).toContain("synthetic invalid");
    await replace(wrapper);
    await wrapper
      .find('[aria-label="管理员QQ机器人AppID"]')
      .setValue("87654321");
    await wrapper
      .find('[aria-label="管理员QQ机器人AppSecret"]')
      .setValue("synthetic231");
    request.mockRejectedValueOnce(new ApiError("synthetic conflict", 409, -1));
    await submit(wrapper);
    expect(wrapper.text()).toContain("不会自动重试");
  });

  it("registers the live draft and discards private state on session change despite a late response", async () => {
    let dirty = () => false;
    let { wrapper, request } = setup(
      "qqbot",
      configured(),
      {},
      {
        register: (_: string, fn: () => boolean) => {
          dirty = fn;
          return () => {};
        },
      },
    );
    await flushPromises();
    await replace(wrapper);
    await wrapper
      .find('[aria-label="管理员QQ机器人AppID"]')
      .setValue("87654321");
    await wrapper
      .find('[aria-label="管理员QQ机器人AppSecret"]')
      .setValue("late-private231");
    expect(dirty()).toBe(true);
    let resolve!: (v: unknown) => void;
    request.mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await wrapper.findComponent(KitModal).find("form").trigger("submit");
    await wrapper.setProps({ ownSession: false });
    resolve({ ...configured(), revision: "9", bound: true });
    await flushPromises();
    expect(wrapper.find("form").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("已保存");
    expect(wrapper.emitted("busy")?.at(-1)).toEqual([false]);
  });
});
