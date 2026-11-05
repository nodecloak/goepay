import { pageReload248 } from "./helpers/page-reload248";
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import Settings from "../src/components/KYCSettingsPanel233.vue";
import Merchant from "../src/components/MerchantCertification233.vue";
import Recognition from "../src/components/OCRRecognition233.vue";
import KitSelect from "../src/components/KitSelect.vue";
import CredentialActionControl from "../src/components/CredentialActionControl.vue";
import KitModal from "../src/components/KitModal.vue";
import { ApiError } from "../src/api";
import {
  certificationCredentials233,
  parseConfig233,
  parseInfo233,
  recognizeOCR233,
  safeCertificationURL233,
} from "../src/kyc233";
import { viewKind } from "../src/menu";
import { deferredPages } from "../src/deferred";
import { settingsDraftRegistry212Key } from "../src/settings-drafts212";
import { kycConfig233, kycInfo233 } from "./fixtures/kyc233";
vi.mock("qrcode", () => ({
  default: {
    toDataURL: vi.fn().mockResolvedValue("data:image/png;base64,synthetic233"),
  },
}));
const wrappers: VueWrapper[] = [];
const pending = <T>() => {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
};
function setupSettings(
  kind: "certification" | "ocr" = "certification",
  extra = kycConfig233(),
  props: Record<string, unknown> = {},
) {
  const request = vi.fn().mockResolvedValue(extra),
    client = { area: "admin", csrfToken: "synthetic233", request };
  let wrapper = mount(Settings, {
    props: {
      client: client as never,
      kind,
      ownSession: true,
      adminClass: "super",
      ...props,
    },
    global: { stubs: { teleport: true } },
  });
  wrappers.push(wrapper);
  return { wrapper, request, client };
}
function setupMerchant(
  extra = kycInfo233(),
  props: Record<string, unknown> = {},
) {
  const request = vi.fn().mockResolvedValue(extra),
    client = { area: "merchant", csrfToken: "synthetic233", request };
  let wrapper = mount(Merchant, {
    props: {
      client: client as never,
      merchantID: "1000",
      ownSession: true,
      ...props,
    },
    global: { stubs: { teleport: true, RouterLink: true } },
  });
  wrappers.push(wrapper);
  return { wrapper, request, client };
}
async function choose(wrapper: VueWrapper, label: string, value: string) {
  const select = wrapper
    .findAllComponents(KitSelect)
    .find((c) => c.props("ariaLabel") === label);
  if (!select) throw Error(`missing select ${label}`);
  select.vm.$emit("update:modelValue", value);
  await flushPromises();
}
async function editCredential(wrapper: VueWrapper, label: string) {
  const control = wrapper
    .findAllComponents(CredentialActionControl)
    .find((c) => c.props("label") === label);
  if (!control) throw Error(`missing credential ${label}`);
  await control.get("button").trigger("click");
  await flushPromises();
}
function button(wrapper: VueWrapper, text: string) {
  const b = wrapper.findAll("button").find((b) => b.text() === text);
  if (!b) throw Error(`missing button ${text}`);
  return b;
}
async function confirm(wrapper: VueWrapper) {
  wrapper.findComponent(KitModal).vm.$emit("submit");
  await flushPromises();
}
async function identity(wrapper: VueWrapper) {
  await wrapper.find('[aria-label="真实姓名"]').setValue("测试人员");
  await wrapper
    .find('[aria-label="身份证号码"]')
    .setValue("11010519491231002X");
  await wrapper.find('[aria-label="同意实名认证及成功扣费"]').setValue(true);
}
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  window.history.replaceState({}, "", "/");
});

describe("certification233 response boundaries", () => {
  it("owns the merchant route and no longer presents the agreed plugin deferral", () => {
    expect(viewKind("merchant", "/account/certificate")).toBe("certification");
    expect(deferredPages.merchant.has("/account/certificate")).toBe(false);
  });
  it.each([
    "javascript:alert(1)",
    "http://faceid.qq.com/",
    "https://faceid.qq.com.evil.invalid/",
    "https://u:p@faceid.qq.com/",
    "https://faceid.qq.com:8443/",
    "https://127.0.0.1/",
  ])("rejects unsafe provider URL %s", (url) =>
    expect(safeCertificationURL233(url)).toBe(false),
  );
  it.each([
    "https://openapi.alipay.com/gateway.do?certify_id=synthetic",
    "https://picker.antcloudauth.aliyuncs.com/gateway.do?certifyId=synthetic",
    "https://open.weixin.qq.com/connect/oauth2/authorize?state=synthetic#wechat_redirect",
  ])("accepts fixed documented provider URL %s", (url) =>
    expect(safeCertificationURL233(url)).toBe(true),
  );
  it("rejects missing credential metadata and unsafe result URLs", () => {
    const config = kycConfig233();
    delete (config.credentials as Record<string, unknown>).cert_qcloudruleid;
    expect(() => parseConfig233(config)).toThrow();
    expect(() =>
      parseInfo233(kycInfo233({ url: "https://attacker.invalid/identity" })),
    ).toThrow();
  });
});

describe("original-console certification and OCR settings", () => {
  it("cancels a private edit without changing the saved mode and explains required-credential clearing", async () => {
    const config = kycConfig233({ cert_open: 2 });
    config.credentials.cert_appcode = { present: true, masked: "***233" };
    let { wrapper, request } = setupSettings("certification", config);
    await flushPromises();
    expect(
      wrapper
        .get('[aria-label="清除手机号三要素AppCode"]')
        .attributes("disabled"),
    ).toBeDefined();
    expect(wrapper.text()).toContain("请先停用当前服务");
    await editCredential(wrapper, "手机号三要素AppCode");
    await wrapper
      .get('[aria-label="手机号三要素AppCode"]')
      .setValue("private-cancel233");
    await wrapper
      .get('[aria-label="取消修改手机号三要素AppCode"]')
      .trigger("click");
    expect(
      wrapper.get('[aria-label="手机号三要素AppCode"]').element,
    ).toHaveProperty("value", "");
    expect(
      button(wrapper, "保存实名认证配置").attributes("disabled"),
    ).toBeDefined();
    expect(request).toHaveBeenCalledTimes(1);
    await choose(wrapper, "是否开启实名认证", "0");
    await button(wrapper, "管理其它服务凭据项").trigger("click");
    await wrapper
      .get('[aria-label="清除手机号三要素AppCode"]')
      .trigger("click");
    expect(wrapper.text()).toContain("保存后清除");
    await wrapper
      .get('[aria-label="撤销清除手机号三要素AppCode"]')
      .trigger("click");
    expect(wrapper.text()).not.toContain("保存后清除");
    expect(request).toHaveBeenCalledTimes(1);
  });
  it.each([{ ownSession: false }, { adminClass: "operator" }])(
    "does not load or edit credentials without original super session %j",
    async (props) => {
      let { wrapper, request } = setupSettings(
        "certification",
        kycConfig233(),
        props,
      );
      await flushPromises();
      expect(request).not.toHaveBeenCalled();
      expect(wrapper.find("form").exists()).toBe(false);
    },
  );
  it.each([
    ["1", ["支付宝通道选择"]],
    ["3", ["支付宝通道选择"]],
    ["2", ["手机号三要素AppCode"]],
    ["4", ["腾讯云SecretId", "腾讯云SecretKey", "腾讯云RuleId"]],
    ["5", ["阿里云AccessKey ID", "阿里云AccessKey Secret", "阿里云场景ID"]],
  ])(
    "shows every required conditional field for mode %s",
    async (mode, fields) => {
      let { wrapper } = setupSettings();
      await flushPromises();
      await choose(wrapper, "是否开启实名认证", mode as string);
      for (const field of fields as string[])
        expect(wrapper.find(`[aria-label="${field}"]`).exists()).toBe(true);
      expect(
        wrapper
          .findAll('input[type="password"]')
          .every((i) => (i.element as HTMLInputElement).value === ""),
      ).toBe(true);
    },
  );
  it("refuses enabling incomplete credentials before issuing a write", async () => {
    let { wrapper, request } = setupSettings();
    await flushPromises();
    await choose(wrapper, "是否开启实名认证", "4");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(wrapper.text()).toContain("完整凭据");
    expect(request).toHaveBeenCalledTimes(1);
    expect(wrapper.findComponent(KitModal).exists()).toBe(false);
  });
  it("saves complete CAS input while preserving unrelated OCR config and clears typed secrets", async () => {
    const config = kycConfig233({ revision: "8", ocr_type: "baidu" }),
      { wrapper, request } = setupSettings("certification", config);
    await flushPromises();
    await choose(wrapper, "是否开启实名认证", "2");
    await editCredential(wrapper, "手机号三要素AppCode");
    await wrapper
      .find('[aria-label="手机号三要素AppCode"]')
      .setValue("synthetic-private233");
    request.mockResolvedValueOnce(
      kycConfig233({ ...config, revision: "9", cert_open: 2 }),
    );
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    const [path, options] = request.mock.calls[1]!;
    expect(path).toBe("/settings/certification");
    expect(options.body).toMatchObject({
      expected_revision: "8",
      cert_open: 2,
      ocr_type: "baidu",
      credentials: {
        cert_appcode: { action: "replace", value: "synthetic-private233" },
      },
    });
    expect(Object.keys(options.body.credentials)).toHaveLength(
      certificationCredentials233.length,
    );
    expect(wrapper.html()).not.toContain("synthetic-private233");
    expect(wrapper.text()).toContain("配置已保存");
  });
  it("blocks ambiguous saves until an explicit read, and does not retry", async () => {
    let { wrapper, request } = setupSettings();
    await flushPromises();
    await wrapper.find('[aria-label="实名认证费用"]').setValue("1.20");
    request.mockRejectedValueOnce(new TypeError("transport failed"));
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    expect(wrapper.text()).toContain("不会自动重试");
    expect(
      button(wrapper, "保存实名认证配置").attributes("disabled"),
    ).toBeDefined();
    expect(request).toHaveBeenCalledTimes(2);
    wrapper = await pageReload248(wrapper);
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(3);
  });
  it("requires OCR provider credentials and retains certification fields", async () => {
    const config = kycConfig233({ cert_open: 1, cert_channel: "31" }),
      { wrapper, request } = setupSettings("ocr", config);
    await flushPromises();
    await choose(wrapper, "文字识别接口", "baidu");
    for (const label of ["百度云API Key", "百度云Secret Key"]) {
      await editCredential(wrapper, label);
      await wrapper
        .find(`[aria-label="${label}"]`)
        .setValue(`synthetic-${label}`);
    }
    request.mockResolvedValueOnce({
      ...config,
      revision: "1",
      ocr_type: "baidu",
    });
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    expect(request.mock.calls[1]![1].body).toMatchObject({
      cert_open: 1,
      cert_channel: "31",
      ocr_type: "baidu",
      credentials: {
        ocr_baiduid: { action: "replace" },
        ocr_baidukey: { action: "replace" },
      },
    });
  });
  it("ignores an old response after session replacement", async () => {
    const delayed = pending<unknown>(),
      request = vi.fn().mockReturnValueOnce(delayed.promise);
    let wrapper = mount(Settings, {
      props: {
        client: { area: "admin", csrfToken: "old233", request } as never,
        kind: "certification",
        ownSession: true,
        adminClass: "super",
      },
    });
    wrappers.push(wrapper);
    await wrapper.setProps({ ownSession: false });
    delayed.resolve(kycConfig233({ revision: "77" }));
    await flushPromises();
    expect(wrapper.find("form").exists()).toBe(false);
    expect(wrapper.text()).toContain("本人超级管理员");
  });
  it("can clear inactive provider credentials without enabling a service", async () => {
    const config = kycConfig233();
    config.credentials.cert_qcloudkey = { present: true, masked: "***233" };
    let { wrapper, request } = setupSettings("certification", config);
    await flushPromises();
    await button(wrapper, "管理其它服务凭据项").trigger("click");
    await wrapper.get('[aria-label="清除腾讯云SecretKey"]').trigger("click");
    request.mockResolvedValueOnce(kycConfig233({ revision: "1" }));
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    expect(request.mock.calls[1]![1].body).toMatchObject({
      cert_open: 0,
      credentials: {
        cert_qcloudkey: { action: "clear", value: "" },
        cert_aliyunkey: { action: "keep", value: "" },
      },
    });
  });
  it("refreshes the shared snapshot when a clean cached panel becomes active again", async () => {
    let { wrapper, request } = setupSettings(
      "certification",
      kycConfig233({ revision: "1" }),
      { active: true },
    );
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    await wrapper.setProps({ active: false });
    request.mockResolvedValueOnce(
      kycConfig233({ revision: "2", ocr_type: "baidu" }),
    );
    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(2);
    await wrapper.find('[aria-label="实名认证费用"]').setValue("0.10");
    request.mockResolvedValueOnce(
      kycConfig233({ revision: "3", cert_money: "0.10", ocr_type: "baidu" }),
    );
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    expect(request.mock.calls[2]![1].body).toMatchObject({
      expected_revision: "2",
      cert_money: "0.10",
      ocr_type: "baidu",
    });
    await wrapper.setProps({ active: false });
    expect(wrapper.text()).not.toContain("配置已保存。");
  });
  it("keeps credential drafts when reactivated and requires explicit confirmed reload before saving", async () => {
    const request = vi.fn().mockResolvedValue(kycConfig233({ revision: "1" })),
      confirmDiscard = vi.fn().mockResolvedValue(false);
    let wrapper = mount(Settings, {
      props: {
        client: { area: "admin", csrfToken: "activation233", request } as never,
        kind: "certification",
        ownSession: true,
        adminClass: "super",
        active: true,
      },
      global: {
        stubs: { teleport: true },
        provide: {
          [settingsDraftRegistry212Key as symbol]: {
            register: () => () => {},
            confirmDiscard,
          },
        },
      },
    });
    wrappers.push(wrapper);
    await flushPromises();
    await choose(wrapper, "是否开启实名认证", "2");
    await editCredential(wrapper, "手机号三要素AppCode");
    await wrapper
      .find('[aria-label="手机号三要素AppCode"]')
      .setValue("retained-private-draft233");
    await wrapper.setProps({ active: false });
    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("草稿已保留");
    expect(
      wrapper.find('[aria-label="手机号三要素AppCode"]').element,
    ).toHaveProperty("value", "retained-private-draft233");
    expect(
      button(wrapper, "保存实名认证配置").attributes("disabled"),
    ).toBeDefined();
    await wrapper.find("form").trigger("submit");
    expect(wrapper.findComponent(KitModal).exists()).toBe(false);
    expect(
      wrapper.findAll("button").some((b) => b.text().includes("重新读取")),
    ).toBe(false);
    expect(confirmDiscard).not.toHaveBeenCalled();
    expect(request).toHaveBeenCalledTimes(1);
    expect(
      wrapper.find('[aria-label="手机号三要素AppCode"]').element,
    ).toHaveProperty("value", "retained-private-draft233");
    request.mockResolvedValueOnce(
      kycConfig233({ revision: "2", ocr_type: "baidu" }),
    );
    wrapper = await pageReload248(wrapper);
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).not.toContain("草稿已保留");
    expect(wrapper.find('[aria-label="手机号三要素AppCode"]').exists()).toBe(
      false,
    );
  });
  it("does not implicitly clear an unknown save when a cached panel becomes active", async () => {
    let { wrapper, request } = setupSettings("certification", kycConfig233(), {
      active: true,
    });
    await flushPromises();
    await wrapper.find('[aria-label="实名认证费用"]').setValue("0.20");
    request.mockRejectedValueOnce(new TypeError("unknown outcome"));
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    await wrapper.setProps({ active: false });
    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("草稿已保留");
    expect(wrapper.text()).toContain("不会自动重试");
    expect(
      button(wrapper, "保存实名认证配置").attributes("disabled"),
    ).toBeDefined();
  });
  it("preserves standalone loading behavior and waits for activation only when explicitly inactive", async () => {
    let { wrapper, request } = setupSettings("ocr", kycConfig233(), {
      active: false,
    });
    await flushPromises();
    expect(request).not.toHaveBeenCalled();
    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
  });
});

describe("merchant certification business interaction", () => {
  it("does not read or submit private identity from administrator delegated sessions", async () => {
    let { wrapper, request } = setupMerchant(kycInfo233(), {
      ownSession: false,
    });
    await flushPromises();
    expect(request).not.toHaveBeenCalled();
    expect(wrapper.find("form").exists()).toBe(false);
  });
  it("renders six Alipay document types and immutable bound phone for three-factor mode", async () => {
    let { wrapper } = setupMerchant();
    await flushPromises();
    expect(wrapper.findComponent(KitSelect).props("options")).toHaveLength(6);
    const phone = setupMerchant(kycInfo233({ cert_open: 2 }));
    await flushPromises();
    expect(
      phone.wrapper.find('[aria-label="已绑定手机号"]').attributes("disabled"),
    ).toBeDefined();
    expect(
      phone.wrapper.find('[aria-label="已绑定手机号"]').element,
    ).toHaveProperty("value", "138****0001");
  });
  it("requires explicit consent, submits all original fields and does not initialize automatically", async () => {
    let { wrapper, request } = setupMerchant();
    await flushPromises();
    await identity(wrapper);
    request.mockResolvedValueOnce(
      kycInfo233({ attempt_id: "a".repeat(32), status: "ready" }),
    );
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    await confirm(wrapper);
    expect(request.mock.calls[1]).toMatchObject([
      "/account/certificate/submit",
      {
        method: "POST",
        body: {
          certtype: 0,
          certcardtype: 0,
          certname: "测试人员",
          certno: "11010519491231002X",
          certcorpname: "",
          certcorpno: "",
          consent: true,
        },
      },
    ]);
    expect(wrapper.html()).not.toContain("11010519491231002X");
    expect(button(wrapper, "开始身份核验").exists()).toBe(true);
    expect(request).toHaveBeenCalledTimes(2);
  });
  it("never repeats an unknown initialization and permits explicit query using the same attempt", async () => {
    const base = kycInfo233({ attempt_id: "b".repeat(32), status: "ready" }),
      { wrapper, request } = setupMerchant(base);
    await flushPromises();
    request.mockRejectedValueOnce(new TypeError("network failed"));
    await button(wrapper, "开始身份核验").trigger("click");
    await flushPromises();
    await confirm(wrapper);
    expect(wrapper.text()).toContain("不会自动重试");
    expect(
      button(wrapper, "开始身份核验").attributes("disabled"),
    ).toBeDefined();
    expect(request).toHaveBeenCalledTimes(2);
    request.mockResolvedValueOnce({
      ...base,
      status: "pending",
      url: "https://faceid.qq.com/?token=synthetic",
    });
    await button(wrapper, "查询认证结果").trigger("click");
    await flushPromises();
    expect(request.mock.calls[2]![1].body).toEqual({
      attempt_id: "b".repeat(32),
      state: "",
      auth_code: "",
    });
    expect(wrapper.find('a[target="_blank"]').attributes("rel")).toBe(
      "noopener noreferrer",
    );
  });
  it("displays masked certified fields and supports a fresh corporate upgrade without copying masks", async () => {
    let { wrapper } = setupMerchant(
      kycInfo233({
        cert: 1,
        cert_corpopen: true,
        certname: "张**",
        certno: "110***********002X",
        status: "passed",
      }),
    );
    await flushPromises();
    expect(wrapper.text()).toContain("110***********002X");
    await button(wrapper, "升级企业认证").trigger("click");
    await flushPromises();
    expect(wrapper.find('[aria-label="法人姓名"]').element).toHaveProperty(
      "value",
      "",
    );
    expect(wrapper.find('[aria-label="企业名称"]').exists()).toBe(true);
  });
  it("queries server-held OAuth authorization without browser codes and blocks reuse", async () => {
    const id = "c".repeat(32);
    let { wrapper, request } = setupMerchant(
      kycInfo233({
        cert_open: 3,
        attempt_id: id,
        status: "pending",
        auth_code_ready: true,
      }),
    );
    await flushPromises();
    request.mockResolvedValueOnce(
      kycInfo233({
        cert_open: 3,
        attempt_id: id,
        status: "unknown",
        auth_code_used: true,
      }),
    );
    await button(wrapper, "查询认证结果").trigger("click");
    await flushPromises();
    expect(request.mock.calls[1]![1].body).toEqual({
      attempt_id: id,
      state: "",
      auth_code: "",
    });
    expect(
      button(wrapper, "查询认证结果").attributes("disabled"),
    ).toBeDefined();
    expect(wrapper.text()).toContain("本次授权已使用");
    expect(request).toHaveBeenCalledTimes(2);
  });
  it("does not query OAuth when the server has no authorization for this merchant attempt", async () => {
    let { wrapper, request } = setupMerchant(
      kycInfo233({
        cert_open: 3,
        attempt_id: "e".repeat(32),
        status: "pending",
      }),
    );
    await flushPromises();
    expect(
      button(wrapper, "查询认证结果").attributes("disabled"),
    ).toBeDefined();
    expect(request).toHaveBeenCalledTimes(1);
  });
  it.each([1, 4, 5])(
    "queries a returned mode %s attempt once without attaching OAuth-only credentials",
    async (mode) => {
      const id = "9".repeat(32);
      window.history.replaceState(
        {},
        "",
        `/merchant/account/certificate?attempt_id=${id}&state=${"s".repeat(32)}`,
      );
      let { wrapper, request } = setupMerchant(
        kycInfo233({ cert_open: mode, attempt_id: id, status: "pending" }),
      );
      await flushPromises();
      request.mockResolvedValueOnce(
        kycInfo233({
          cert_open: mode,
          attempt_id: id,
          status: "passed",
          cert: 1,
        }),
      );
      await button(wrapper, "查询认证结果").trigger("click");
      await flushPromises();
      expect(request.mock.calls[1]![1].body).toEqual({
        attempt_id: id,
        state: "",
        auth_code: "",
      });
      expect(request).toHaveBeenCalledTimes(2);
      expect(wrapper.text()).toContain("已实名认证");
    },
  );
  it("does not retry a rejected validation request or expose raw provider errors", async () => {
    let { wrapper, request } = setupMerchant();
    await flushPromises();
    await identity(wrapper);
    request.mockRejectedValueOnce(new ApiError("secretPII233", 422, -1));
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    expect(wrapper.text()).not.toContain("secretPII233");
    expect(wrapper.text()).toContain("可用余额");
    expect(request).toHaveBeenCalledTimes(2);
  });
  it("uses the frozen pending attempt for queries after configuration changes and allows reset", async () => {
    let { wrapper, request } = setupMerchant(
      kycInfo233({
        attempt_id: "f".repeat(32),
        status: "pending",
        url: "https://faceid.qq.com/?token=old233",
        configuration_changed: true,
      }),
    );
    await flushPromises();
    expect(wrapper.text()).toContain("认证服务配置或绑定资料已变化");
    expect(
      button(wrapper, "查询认证结果").attributes("disabled"),
    ).toBeUndefined();
    expect(wrapper.find('a[target="_blank"]').exists()).toBe(true);
    await button(wrapper, "重新填写认证资料").trigger("click");
    await flushPromises();
    await confirm(wrapper);
    expect(request.mock.calls[1]![0]).toBe("/account/certificate/reset");
  });
  it("finishes a durably verified attempt without replaying OAuth authorization", async () => {
    const id = "8".repeat(32);
    window.history.replaceState(
      {},
      "",
      `/merchant/account/certificate?attempt_id=${id}&state=${"s".repeat(32)}&auth_code=oldcode233`,
    );
    let { wrapper, request } = setupMerchant(
      kycInfo233({
        cert_open: 3,
        attempt_id: id,
        status: "verified",
        configuration_changed: true,
        cert_money: "1.00",
      }),
    );
    await flushPromises();
    expect(wrapper.text()).toContain("已通过服务商核验");
    request.mockResolvedValueOnce(kycInfo233({ cert: 1, status: "passed" }));
    await button(wrapper, "完成认证").trigger("click");
    await flushPromises();
    expect(request.mock.calls[1]![1].body).toEqual({
      attempt_id: id,
      state: "",
      auth_code: "",
    });
    expect(wrapper.text()).toContain("已实名认证");
  });
  it("shows the server query cooldown without polling or automatically dispatching at expiry", async () => {
    vi.useFakeTimers();
    let { wrapper, request } = setupMerchant(
      kycInfo233({
        attempt_id: "7".repeat(32),
        status: "pending",
        query_after: new Date(Date.now() + 30000).toISOString(),
      }),
    );
    await flushPromises();
    expect(
      button(wrapper, "查询认证结果").attributes("disabled"),
    ).toBeDefined();
    expect(wrapper.text()).toContain("不会自动请求");
    await vi.advanceTimersByTimeAsync(31000);
    await flushPromises();
    expect(
      button(wrapper, "查询认证结果").attributes("disabled"),
    ).toBeUndefined();
    expect(request).toHaveBeenCalledTimes(1);
  });
});

describe("explicit OCR upload and review", () => {
  it("rejects remote paths and oversized/wrong document uploads before network access", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const client = { area: "merchant", csrfToken: "test233" } as never;
    await expect(
      recognizeOCR233(
        client,
        "aliyun",
        "1",
        "path",
        new File(["x"], "x.png", { type: "image/png" }),
        "a".repeat(32),
      ),
    ).rejects.toThrow();
    await expect(
      recognizeOCR233(
        client,
        "aliyun",
        "1",
        "idcard",
        new File(["x"], "x.svg", { type: "image/svg+xml" }),
        "a".repeat(32),
      ),
    ).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
  });
  it("uploads exactly one image, type and one-use operation ID without JSON conversion", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: 0,
          data: { fields: { name: "测试人员", id_no: "synthetic233" } },
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetch);
    const client = { area: "merchant", csrfToken: "test233" } as never,
      file = new File(["synthetic image"], "id.png", { type: "image/png" });
    const out = await recognizeOCR233(
      client,
      "aliyun",
      "1",
      "idcard",
      file,
      "a".repeat(32),
    );
    expect(out.name).toBe("测试人员");
    const [url, options] = fetch.mock.calls[0]!;
    expect(url).toBe("/console/v1/merchant/account/ocr");
    expect(options.headers).toMatchObject({ "X-CSRF-Token": "test233" });
    expect(options.headers).not.toHaveProperty("Content-Type");
    expect([...options.body.keys()]).toEqual([
      "document_type",
      "image",
      "operation_id",
      "expected_provider",
      "expected_revision",
    ]);
    expect(options.body.get("expected_provider")).toBe("aliyun");
    expect(options.body.get("expected_revision")).toBe("1");
  });
  it("rejects a Baidu image larger than its two MiB limit before dispatch", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const file = new File([new Uint8Array(2097153)], "large.png", {
      type: "image/png",
    });
    await expect(
      recognizeOCR233(
        { area: "merchant", csrfToken: "test233" } as never,
        "baidu",
        "1",
        "idcard",
        file,
        "a".repeat(32),
      ),
    ).rejects.toThrow("2MiB");
    expect(fetch).not.toHaveBeenCalled();
  });
  it("requires file selection, consent and confirmation before calling OCR and then requires review", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: 0,
          data: { fields: { name: "测试人员", id_no: "synthetic233" } },
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetch);
    let wrapper = mount(Recognition, {
      props: {
        client: { area: "merchant", csrfToken: "test233" } as never,
        ownSession: true,
        provider: "aliyun",
        configRevision: "1",
      },
      global: { stubs: { teleport: true } },
    });
    wrappers.push(wrapper);
    const input = wrapper.find('input[type="file"]');
    Object.defineProperty(input.element, "files", {
      configurable: true,
      value: [new File(["synthetic image"], "id.png", { type: "image/png" })],
    });
    await input.trigger("change");
    expect(fetch).not.toHaveBeenCalled();
    await wrapper.find('input[type="checkbox"]').setValue(true);
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(fetch).not.toHaveBeenCalled();
    await confirm(wrapper);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(wrapper.emitted("prefill")).toBeUndefined();
    await button(wrapper, "已核对，预填认证表单").trigger("click");
    expect(wrapper.emitted("prefill")?.[0]).toEqual([
      { name: "测试人员", id_no: "synthetic233" },
      "idcard",
    ]);
  });
  it("keeps an unknown OCR result blocked without automatically sending another operation", async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError("offline"));
    vi.stubGlobal("fetch", fetch);
    let wrapper = mount(Recognition, {
      props: {
        client: { area: "merchant", csrfToken: "test233" } as never,
        ownSession: true,
        provider: "baidu",
        configRevision: "1",
      },
      global: { stubs: { teleport: true } },
    });
    wrappers.push(wrapper);
    const input = wrapper.find('input[type="file"]');
    Object.defineProperty(input.element, "files", {
      configurable: true,
      value: [new File(["synthetic image"], "id.png", { type: "image/png" })],
    });
    await input.trigger("change");
    await wrapper.find('input[type="checkbox"]').setValue(true);
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await confirm(wrapper);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("可能已产生费用");
    expect(button(wrapper, "识别证照").attributes("disabled")).toBeDefined();
  });
});
