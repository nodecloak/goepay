import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import IdentityProfileLookup from "../src/components/IdentityProfileLookup.vue";
import { ApiClient } from "../src/api";

const qr = vi.hoisted(() => ({
  toDataURL: vi.fn(
    async (_target: string, _options: unknown) =>
      "data:image/png;base64,c3ludGhldGlj",
  ),
}));
vi.mock("qrcode", () => ({ default: qr }));

const id = "a".repeat(64),
  state = "b".repeat(64);
const profile = {
  id: "11",
  revision: "2",
  channel_id: "7",
  provider: "wxpay",
  kind: "mini",
  app_id: "independent-app",
  path: "pages/identity/index",
  url_link_available: true,
};
const wrappers: ReturnType<typeof mount>[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((w) => w.unmount());
  vi.useRealTimers();
  qr.toDataURL.mockClear();
});
function setup(
  options: {
    failure?: boolean;
    linkStatus?: string;
    short?: boolean;
    responseFlag?: boolean;
  } = {},
) {
  const expires = new Date(
    Date.now() + (options.short ? 70000 : 240000),
  ).toISOString();
  const created = new Date(Date.now() - 500).toISOString();
  const q = {
    request_id: id,
    merchant_id: "1000",
    channel_id: "7",
    profile_id: "11",
    profile_revision: "2",
    provider: "wxpay",
    kind: "mini",
    app_id: profile.app_id,
    path: profile.path,
    status: "pending",
    expires_at: expires,
  };
  const link = (status = "ready") => ({
    purpose: "wechat_mini_identity_url_link",
    request_id: id,
    provider: "wxpay",
    app_id: profile.app_id,
    status,
    created_at: created,
    expires_at: expires,
    requested_expires_at: new Date(
      Math.floor(Date.parse(expires) / 1000) * 1000,
    ).toISOString(),
    ...(status === "ready"
      ? { url_link: "https://wxaurl.cn/synthetic-ticket" }
      : {}),
  });
  const request = vi.fn(async (path: string, input?: { method?: string }) => {
    if (path.includes("/capabilities"))
      return {
        merchant_id: "1000",
        available: true,
        reason: "",
        profiles: [profile],
        url_link_available: true,
      };
    if (path.endsWith("/url-link")) {
      if (input?.method === "POST") {
        if (options.failure) throw Error("private-provider-token");
        return link(options.linkStatus);
      }
      return link("ready");
    }
    if (path.endsWith("/complete"))
      return { request_id: id, status: "verified" };
    if (input?.method === "POST")
      return {
        request: q,
        url_link_available: options.responseFlag ?? true,
        launch: {
          kind: "sdk",
          provider: "wxpay",
          app_id: profile.app_id,
          path: profile.path,
          method: "POST",
          completion_url: `https://pay.example.invalid/identity/profile/mini/wxpay/11/${id}/`,
          body: { state },
          expires_at: expires,
        },
      };
    return q;
  });
  const w = mount(IdentityProfileLookup, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      area: "admin",
      ownSession: true,
    },
  });
  wrappers.push(w);
  return { w, request, link };
}
async function create(w: ReturnType<typeof setup>["w"]) {
  await w.get('input[inputmode="numeric"]').setValue("1000");
  await w
    .findAll("button")
    .find((b) => b.text() === "读取独立应用")!
    .trigger("click");
  await flushPromises();
  await w.get("select").setValue("11");
  await w
    .findAll("button")
    .find((b) => b.text() === "创建独立授权请求")!
    .trigger("click");
  await flushPromises();
}
it("only generates explicitly with the original SDK state and keeps SDK completion independent", async () => {
  const { w, request } = setup();
  await create(w);
  expect(
    request.mock.calls.filter(([path]) => path.endsWith("/url-link")),
  ).toHaveLength(0);
  await w.get('[data-action="profile-link-generate"]').trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    `/identity-profile-lookups/${id}/url-link`,
    expect.objectContaining({ method: "POST", body: { state } }),
  );
  expect(w.get('[data-action="profile-link-open"]').attributes("href")).toBe(
    "https://wxaurl.cn/synthetic-ticket",
  );
  expect(
    w.get('[data-action="profile-link-generate"]').attributes("disabled"),
  ).toBeDefined();
  expect(w.text()).toContain("请求截止值");
  await w.get('input[type="password"]').setValue("local-sdk-code");
  await w
    .findAll("button")
    .find((b) => b.text() === "检查并兑换本次临时码")!
    .trigger("click");
  await w
    .findAll("button")
    .find((b) => b.text() === "确认兑换标识")!
    .trigger("click");
  await flushPromises();
  expect(request).toHaveBeenCalledWith(
    `/identity-profile-lookups/${id}/complete`,
    expect.objectContaining({ body: { state, code: "local-sdk-code" } }),
  );
  expect(w.find('[data-action="profile-link-open"]').exists()).toBe(false);
});
it("encodes only the actual returned URL after explicit QR action and clears it with original authority", async () => {
  const { w, request } = setup();
  await create(w);
  await w.get('[data-action="profile-link-generate"]').trigger("click");
  await flushPromises();
  expect(qr.toDataURL).not.toHaveBeenCalled();
  await w.get('[data-action="profile-link-qr"]').trigger("click");
  await flushPromises();
  expect(qr.toDataURL).toHaveBeenCalledWith(
    "https://wxaurl.cn/synthetic-ticket",
    { width: 240, margin: 2 },
  );
  expect(
    w.get('img[alt="本次微信小程序身份查询二维码"]').attributes("src"),
  ).toBe("data:image/png;base64,c3ludGhldGlj");
  request.mockRejectedValueOnce(Error("original identity unavailable"));
  await w
    .findAll("button")
    .find((b) => b.text() === "查询独立授权状态")!
    .trigger("click");
  await flushPromises();
  expect(w.find('img[alt="本次微信小程序身份查询二维码"]').exists()).toBe(
    false,
  );
});
it("discards a late local QR render after the original merchant context changes", async () => {
  const { w } = setup();
  await create(w);
  await w.get('[data-action="profile-link-generate"]').trigger("click");
  await flushPromises();
  let finish!: (value: string) => void;
  qr.toDataURL.mockImplementationOnce(
    () =>
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
  );
  await w.get('[data-action="profile-link-qr"]').trigger("click");
  await w.get('input[inputmode="numeric"]').setValue("1001");
  finish("data:image/png;base64,c3ludGhldGlj");
  await flushPromises();
  expect(w.find('img[alt="本次微信小程序身份查询二维码"]').exists()).toBe(
    false,
  );
});
it("preserves the one-attempt generation barrier after network uncertainty and a ready local GET", async () => {
  const { w, request } = setup({ failure: true });
  await create(w);
  await w.get('[data-action="profile-link-generate"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("URL Link生成结果不确定");
  expect(w.text()).not.toContain("private-provider-token");
  expect(
    w.get('[data-action="profile-link-generate"]').attributes("disabled"),
  ).toBeDefined();
  await w.get('[data-action="profile-link-query"]').trigger("click");
  await flushPromises();
  expect(w.get('[data-action="profile-link-open"]').attributes("href")).toBe(
    "https://wxaurl.cn/synthetic-ticket",
  );
  await w.get('[data-action="profile-link-generate"]').trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(
      ([path, input]) => path.endsWith("/url-link") && input?.method === "POST",
    ),
  ).toHaveLength(1);
  const get = request.mock.calls.find(
    ([path, input]) => path.endsWith("/url-link") && input?.method !== "POST",
  );
  expect(get?.[1]).not.toHaveProperty("body");
  expect(localStorage.length + sessionStorage.length).toBe(0);
});
it("cannot generate while the original requested cutoff has at most70 seconds remaining", async () => {
  const { w, request } = setup({ short: true });
  await create(w);
  expect(
    w.get('[data-action="profile-link-generate"]').attributes("disabled"),
  ).toBeDefined();
  expect(
    request.mock.calls.filter(([path]) => path.endsWith("/url-link")),
  ).toHaveLength(0);
});
it("clears a ready URL when original identity authority can no longer be read, without restoring generation", async () => {
  const { w, request } = setup();
  await create(w);
  await w.get('[data-action="profile-link-generate"]').trigger("click");
  await flushPromises();
  expect(w.find('[data-action="profile-link-open"]').exists()).toBe(true);
  request.mockRejectedValueOnce(
    Error("original identity authority unavailable"),
  );
  await w
    .findAll("button")
    .find((b) => b.text() === "查询独立授权状态")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[data-action="profile-link-open"]').exists()).toBe(false);
  expect(
    w.get('[data-action="profile-link-generate"]').attributes("disabled"),
  ).toBeDefined();
});
it("rejects a create capability mismatch without enabling a link and retains existing create uncertainty", async () => {
  const { w } = setup({ responseFlag: false });
  await create(w);
  expect(w.find('[data-action="profile-link-generate"]').exists()).toBe(false);
  expect(w.text()).toContain("创建响应未知");
});
it("clears returned link on context changes and original expiry without polling", async () => {
  vi.useFakeTimers();
  const { w, request } = setup();
  await create(w);
  await w.get('[data-action="profile-link-generate"]').trigger("click");
  await flushPromises();
  expect(w.find('[data-action="profile-link-open"]').exists()).toBe(true);
  const before = request.mock.calls.length;
  vi.advanceTimersByTime(241000);
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(before);
  expect(w.find('[data-action="profile-link-open"]').exists()).toBe(false);
  await w.get('input[inputmode="numeric"]').setValue("1001");
  expect(w.find('[data-action="profile-link-query"]').exists()).toBe(false);
});
it("keeps every creating, failed or unknown intent locked while manual SDK completion remains available", async () => {
  for (const status of ["creating", "failed", "unknown"]) {
    const { w, request } = setup({ linkStatus: status });
    await create(w);
    await w.get('[data-action="profile-link-generate"]').trigger("click");
    await flushPromises();
    expect(
      w.get('[data-action="profile-link-generate"]').attributes("disabled"),
    ).toBeDefined();
    expect(w.find('[data-action="profile-link-open"]').exists()).toBe(false);
    expect(w.find('input[type="password"]').exists()).toBe(true);
    expect(
      request.mock.calls.filter(
        ([path, input]) =>
          path.endsWith("/url-link") && input?.method === "POST",
      ),
    ).toHaveLength(1);
  }
});
it("clears returned URLs immediately on merchant, profile, session and client changes", async () => {
  for (const change of ["merchant", "profile", "session", "client"]) {
    const { w } = setup();
    await create(w);
    await w.get('[data-action="profile-link-generate"]').trigger("click");
    await flushPromises();
    expect(w.find('[data-action="profile-link-open"]').exists()).toBe(true);
    if (change === "merchant")
      await w.get('input[inputmode="numeric"]').setValue("1001");
    if (change === "profile") await w.get("select").setValue("");
    if (change === "session") await w.setProps({ ownSession: false });
    if (change === "client")
      await w.setProps({ client: new ApiClient("admin") });
    expect(w.find('[data-action="profile-link-open"]').exists()).toBe(false);
    expect(w.find('[data-action="profile-link-query"]').exists()).toBe(false);
  }
});
it("ignores a late generation response after a context change or unmount", async () => {
  for (const change of ["merchant", "unmount"]) {
    const { w, request, link } = setup();
    await create(w);
    let finish!: (value: ReturnType<typeof link>) => void;
    request.mockImplementationOnce(
      () => new Promise((resolve) => (finish = resolve)),
    );
    await w.get('[data-action="profile-link-generate"]').trigger("click");
    if (change === "merchant")
      await w.get('input[inputmode="numeric"]').setValue("1001");
    else w.unmount();
    finish(link());
    await flushPromises();
    expect(w.find('[data-action="profile-link-open"]').exists()).toBe(false);
    expect(w.html()).not.toContain("https://wxaurl.cn/synthetic-ticket");
  }
});
