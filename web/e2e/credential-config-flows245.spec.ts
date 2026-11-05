import { pageReload248 } from "./fixtures/page-reload248";
import { expect, test, type Locator, type Page } from "@playwright/test";
import fs from "node:fs";
import { kycConfig233 } from "../tests/fixtures/kyc233";

// These are browser tests of the compiled app with a stateful, local API twin.
// No provider, test-send, binding or payment endpoint is allowed to execute.
type Write = { path: string; method: string; body: any };
type Handler = (path: string, method: string, body: any) => unknown;
const fp = (version: number) => version.toString(16).padStart(64, "0");
async function api(page: Page, area: "admin" | "merchant", handler: Handler) {
  const writes: Write[] = [],
    reads: string[] = [],
    external: string[] = [];
  const origin = new URL(String(test.info().project.use.baseURL)).origin;
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (url.origin !== origin) {
      external.push(url.origin + url.pathname);
      return route.abort();
    }
    if (!url.pathname.startsWith("/console/v1/")) return route.continue();
    const path = url.pathname.replace(/^\/console\/v1\/(admin|merchant)/, ""),
      method = request.method(),
      body = method === "GET" ? undefined : request.postDataJSON();
    if (method === "GET") reads.push(path);
    else writes.push({ path, method, body });
    let data = handler(path, method, body);
    if (data === undefined && method !== "GET")
      return route.fulfill({
        status: 422,
        json: { code: -1, message: "Unexpected synthetic mutation" },
      });
    if (data === undefined) {
      if (path === "/session")
        data = {
          user: {
            id: area === "admin" ? "168" : "1",
            name: "Synthetic245",
            role: area,
            ...(area === "admin" ? { admin_class: "super" } : {}),
          },
          csrf_token: "synthetic245",
        };
      else if (path === "/resources/settings/schema")
        data = {
          title: "设置",
          fields: [
            {
              name: "key",
              type: "select",
              options: [
                "site_name",
                "msgconfig_complain_all",
                "msgconfig_channellimit",
              ],
            },
          ],
          permissions: { create: true, update: true, delete: false },
        };
      else if (path === "/resources/settings")
        data = {
          items: [{ id: "1", key: "site_name", value: "Synthetic245" }],
          total: 1,
        };
      else if (path === "/security/payment-password")
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (/\/notice-accounts\/(robot|telegram|qqbot)$/.test(path))
        data = botState();
      else if (path === "/settings/voice" || path === "/printing/settings")
        data = hardwareState();
      else data = { items: [], total: 0, next_cursor: "" };
    }
    return route.fulfill({ json: { code: 0, message: "", data } });
  });
  return { writes, reads, external };
}
async function choose(scope: Page | Locator, label: string, option: string) {
  await scope.getByRole("button", { name: label, exact: true }).click();
  await scope.getByRole("option", { name: option, exact: true }).click();
}
async function confirm(page: Page, title: string) {
  await page
    .getByRole("dialog", { name: title, exact: true })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
}
async function privateState(
  page: Page,
  requests: Awaited<ReturnType<typeof api>>,
  secrets: string[],
) {
  expect(requests.external).toEqual([]);
  const storage = await page.evaluate(() =>
    JSON.stringify({
      local: { ...localStorage },
      session: { ...sessionStorage },
    }),
  );
  for (const secret of secrets) {
    expect(storage).not.toContain(secret);
    expect(
      await page
        .locator("input,textarea")
        .evaluateAll((nodes) =>
          nodes.map((node) => (node as HTMLInputElement).value),
        ),
    ).not.toContain(secret);
  }
}
function hardwareState() {
  return {
    revision: "3",
    enabled: false,
    app_id_masked: "old***245",
    secret_present: true,
    provider: "local_fake",
    external_execution_available: true,
  };
}
function botState() {
  return {
    revision: "3",
    enabled: false,
    app_id_masked: "old***245",
    secret_present: true,
    bound: false,
    recipient_masked: "",
    phone_masked: "",
    token_source: "dedicated",
  };
}

for (const kind of ["voice", "printing"] as const) {
  test(`hardware ${kind}: private cancel, clear undo, confirmed replacement and clear survive readback`, async ({
    page,
  }) => {
    let state = hardwareState();
    const path = kind === "voice" ? "/settings/voice" : "/printing/settings";
    const requests = await api(page, "admin", (p, method, body) => {
      if (p !== path) return;
      if (method === "PUT") {
        expect(body.expected_revision).toBe(state.revision);
        state = {
          ...state,
          revision: String(Number(state.revision) + 1),
          enabled: body.enabled,
          secret_present: body.credential_action === "clear" ? false : true,
          app_id_masked: body.credential_action === "clear" ? "" : "new***245",
        };
      }
      return state;
    });
    await page.goto("/admin/system/settings?tab=notice");
    const title = kind === "voice" ? "云喇叭语音播报" : "订单小票打印",
      panel = page.getByRole("region", { name: title, exact: true }),
      label = title + "凭据",
      app = panel.getByLabel(kind === "voice" ? "云喇叭agent_id" : "APPID", {
        exact: true,
      }),
      secret = panel.getByLabel(
        kind === "voice" ? "云喇叭agent_secret" : "APPSECRET",
        { exact: true },
      ),
      save = panel.getByRole("button", {
        name: kind === "voice" ? "保存云喇叭配置" : "保存小票打印配置",
        exact: true,
      });
    await panel
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await app.fill("discarded-app245");
    await secret.fill("discarded-hardware245");
    await panel
      .getByRole("button", { name: "取消修改" + label, exact: true })
      .click();
    await expect(app).toHaveValue("");
    await expect(secret).toHaveValue("");
    await panel
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await expect(panel.getByText("保存后清除", { exact: true })).toBeVisible();
    await panel
      .getByRole("button", { name: "撤销清除" + label, exact: true })
      .click();
    expect(requests.writes).toEqual([]);
    await panel.getByRole("switch").click();
    await expect(
      panel.getByRole("button", { name: "清除" + label, exact: true }),
    ).toBeDisabled();
    await panel.getByRole("switch").click();
    await panel
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await app.fill("new-hardware-app245");
    await secret.fill("new-hardware-secret245");
    await save.click();
    expect(requests.writes).toHaveLength(0);
    await confirm(page, "确认保存" + title);
    await expect(panel.getByText(/配置已保存/)).toBeVisible();
    expect(requests.writes).toEqual([
      {
        path,
        method: "PUT",
        body: {
          expected_revision: "3",
          enabled: false,
          credential_action: "replace",
          app_id: "new-hardware-app245",
          secret: "new-hardware-secret245",
          ...(kind === "printing" ? { provider: "local_fake" } : {}),
        },
      },
    ]);
    await pageReload248(page);
    await expect(
      panel.getByRole("button", { name: "修改" + label, exact: true }),
    ).toBeVisible();
    await panel
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await save.click();
    expect(requests.writes).toHaveLength(1);
    await confirm(page, "确认保存" + title);
    await expect(
      panel.getByRole("button", { name: "设置" + label, exact: true }),
    ).toBeVisible();
    expect(requests.writes[1].body).toEqual({
      expected_revision: "4",
      enabled: false,
      credential_action: "clear",
      app_id: "",
      secret: "",
      ...(kind === "printing" ? { provider: "local_fake" } : {}),
    });
    expect(requests.writes).toHaveLength(2);
    await privateState(page, requests, [
      "discarded-hardware245",
      "new-hardware-secret245",
    ]);
  });
}

for (const kind of ["robot", "telegram", "qqbot"] as const) {
  test(`bot ${kind}: cancel and undo are local; exact replacement and clear never start binding or send`, async ({
    page,
  }) => {
    let state = {
      ...botState(),
      ...(kind === "qqbot"
        ? { bound: true, recipient_masked: "old-recipient***" }
        : {}),
    };
    const path = "/settings/notice-accounts/" + kind;
    const requests = await api(page, "admin", (p, method, body) => {
      if (p !== path) return;
      if (method === "PUT") {
        expect(body.expected_revision).toBe(state.revision);
        state = {
          ...state,
          revision: String(Number(state.revision) + 1),
          enabled: body.enabled,
          secret_present: body.credential_action !== "clear",
          app_id_masked: body.credential_action === "clear" ? "" : "new***245",
          phone_masked: body.phone ? "138***245" : "",
          token_source: body.token_source,
          bound: false,
          recipient_masked: "",
        };
      }
      return state;
    });
    await page.goto("/admin/system/settings?tab=notice");
    const title =
        kind === "robot"
          ? "管理员群机器人配置"
          : kind === "telegram"
            ? "管理员Telegram配置"
            : "管理员QQ机器人配置",
      panel = page.getByRole("region", { name: title, exact: true }),
      label = title + "凭据",
      accountLabel =
        kind === "robot"
          ? "群机器人Webhook地址"
          : kind === "telegram"
            ? "管理员Telegram Chat Id"
            : "管理员QQ机器人AppID",
      account = panel.getByLabel(accountLabel, { exact: true }),
      secret =
        kind === "robot"
          ? panel.getByLabel("管理员手机号码", { exact: true })
          : panel.getByLabel(
              kind === "telegram"
                ? "Telegram Bot Token"
                : "管理员QQ机器人AppSecret",
              { exact: true },
            ),
      save = panel.getByRole("button", { name: "保存" + title, exact: true });
    await panel
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await account.fill("discarded-bot245");
    await secret.fill(
      kind === "robot" ? "13800000245" : "discarded-bot-secret245",
    );
    await panel
      .getByRole("button", { name: "取消修改" + label, exact: true })
      .click();
    await expect(account).toHaveValue("");
    await expect(secret).toHaveValue("");
    await panel
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await panel
      .getByRole("button", { name: "撤销清除" + label, exact: true })
      .click();
    expect(requests.writes).toHaveLength(0);
    const app =
      kind === "robot"
        ? "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic245"
        : kind === "telegram"
          ? "-1000000000245"
          : "123456245";
    await panel
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await account.fill(app);
    if (kind === "telegram")
      await choose(panel, "Telegram Token来源", "复用快捷登录Telegram Token");
    else
      await secret.fill(kind === "robot" ? "13800000245" : "new-bot-secret245");
    await save.click();
    expect(requests.writes).toHaveLength(0);
    await confirm(page, "确认保存" + title);
    await expect(
      panel.getByText("配置已保存。", { exact: true }),
    ).toBeVisible();
    expect(requests.writes[0]).toEqual({
      path,
      method: "PUT",
      body: {
        expected_revision: "3",
        enabled: false,
        credential_action: "replace",
        app_id: app,
        secret: kind === "qqbot" ? "new-bot-secret245" : "",
        phone: kind === "robot" ? "13800000245" : "",
        token_source: kind === "telegram" ? "oauth" : "",
      },
    });
    await pageReload248(page);
    await expect(
      panel.getByRole("button", { name: "修改" + label, exact: true }),
    ).toBeVisible();
    if (kind === "qqbot")
      await expect(panel.getByText("未绑定", { exact: true })).toBeVisible();
    await panel
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await save.click();
    expect(requests.writes).toHaveLength(1);
    await confirm(page, "确认保存" + title);
    await expect(
      panel.getByRole("button", { name: "设置" + label, exact: true }),
    ).toBeVisible();
    expect(requests.writes[1].body).toEqual({
      expected_revision: "4",
      enabled: false,
      credential_action: "clear",
      app_id: "",
      secret: "",
      phone: "",
      token_source: "",
    });
    expect(requests.writes).toHaveLength(2);
    await privateState(page, requests, [
      "discarded-bot245",
      "discarded-bot-secret245",
      "new-bot-secret245",
    ]);
  });
}

for (const area of ["admin", "merchant"] as const) {
  test(`${area} printing: replacement cancel, staged clear undo, exact confirmed save and re-read`, async ({
    page,
  }) => {
    let state: any =
      area === "admin"
        ? hardwareState()
        : {
            revision: "3",
            mode: 0,
            copies: 1,
            voice: false,
            device_present: true,
            device_masked: "old***245",
          };
    const path = area === "admin" ? "/printing/settings" : "/account/printing";
    const requests = await api(page, area, (p, method, body) => {
      if (p !== path) return;
      if (method === "PUT") {
        expect(body.expected_revision).toBe(state.revision);
        const clear =
          (body.credential_action || body.device_action) === "clear";
        state =
          area === "admin"
            ? {
                ...state,
                revision: String(Number(state.revision) + 1),
                enabled: body.enabled,
                provider: body.provider,
                secret_present: !clear,
                app_id_masked: clear ? "" : "new***245",
              }
            : {
                ...state,
                revision: String(Number(state.revision) + 1),
                mode: body.mode,
                copies: body.copies,
                voice: body.voice,
                device_present: !clear,
                device_masked: clear ? "" : "new***245",
              };
      }
      return state;
    });
    await page.goto(
      area === "admin"
        ? "/admin/system/printing"
        : "/merchant/account/printing",
    );
    const label = area === "admin" ? "打印服务凭据" : "打印设备",
      value = area === "admin" ? "应用密钥" : "设备编号";
    await page
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await page.getByLabel(value, { exact: true }).fill("discarded-print245");
    await page
      .getByRole("button", { name: "取消修改" + label, exact: true })
      .click();
    await expect(page.getByLabel(value, { exact: true })).toHaveCount(0);
    await page
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await page
      .getByRole("button", { name: "撤销清除" + label, exact: true })
      .click();
    expect(requests.writes).toHaveLength(0);
    // Revalidate a pending clear if automatic printing is enabled afterward.
    await page
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    if (area === "admin") await page.getByRole("switch").first().click();
    else await choose(page, "自动打印范围", "全部普通支付订单");
    await page
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    await expect(
      page.getByRole("alert").filter({ hasText: "清除" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "确认保存", exact: true }),
    ).toHaveCount(0);
    expect(requests.writes).toHaveLength(0);
    if (area === "admin") await page.getByRole("switch").first().click();
    else await choose(page, "自动打印范围", "不自动打印");
    await page
      .getByRole("button", { name: "撤销清除" + label, exact: true })
      .click();
    await page
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await page
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    // Empty required fields are stopped by the browser before Vue's submit
    // handler runs. Whitespace then exercises the additional application rule.
    expect(
      await page
        .getByLabel(value, { exact: true })
        .evaluate((input) => (input as HTMLInputElement).validity.valueMissing),
    ).toBe(true);
    await expect(
      page.getByRole("button", { name: "确认保存", exact: true }),
    ).toHaveCount(0);
    expect(requests.writes).toHaveLength(0);
    await page.getByLabel(value, { exact: true }).fill(" ");
    if (area === "admin")
      await page.getByLabel("应用ID", { exact: true }).fill(" ");
    await page
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    await expect(
      page
        .getByRole("alert")
        .filter({ hasText: area === "admin" ? "完整" : "设备编号" }),
    ).toBeVisible();
    expect(requests.writes).toHaveLength(0);
    await page.getByLabel(value, { exact: true }).fill("new-print-private245");
    if (area === "admin")
      await page.getByLabel("应用ID", { exact: true }).fill("new-print-app245");
    await page
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    expect(requests.writes).toHaveLength(0);
    await page.getByRole("button", { name: "取消", exact: true }).click();
    await expect(page.getByLabel(value, { exact: true })).toHaveCount(0);
    expect(requests.writes).toHaveLength(0);
    await page
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await page.getByLabel(value, { exact: true }).fill("new-print-private245");
    if (area === "admin")
      await page.getByLabel("应用ID", { exact: true }).fill("new-print-app245");
    await page
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    await page.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect(
      page.getByText("打印设置已保存；保存设置不会立即打印。", { exact: true }),
    ).toBeVisible();
    expect(requests.writes[0]).toEqual({
      path,
      method: "PUT",
      body:
        area === "admin"
          ? {
              expected_revision: "3",
              enabled: false,
              provider: "local_fake",
              credential_action: "replace",
              app_id: "new-print-app245",
              secret: "new-print-private245",
            }
          : {
              expected_revision: "3",
              mode: 0,
              copies: 1,
              voice: false,
              device_action: "replace",
              device_id: "new-print-private245",
            },
    });
    await pageReload248(page);
    await expect(page.getByText(/new\*\*\*245/)).toBeVisible();
    await page
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await page
      .getByRole("button", { name: "检查并保存打印设置", exact: true })
      .click();
    expect(requests.writes).toHaveLength(1);
    await page.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "设置" + label, exact: true }),
    ).toBeVisible();
    expect(requests.writes[1].body).toEqual(
      area === "admin"
        ? {
            expected_revision: "4",
            enabled: false,
            provider: "local_fake",
            credential_action: "clear",
            app_id: "",
            secret: "",
          }
        : {
            expected_revision: "4",
            mode: 0,
            copies: 1,
            voice: false,
            device_action: "clear",
            device_id: "",
          },
    );
    expect(requests.writes).toHaveLength(2);
    await privateState(page, requests, [
      "discarded-print245",
      "new-print-private245",
    ]);
  });
}

for (const area of ["admin", "merchant"] as const) {
  test(`${area} notice recipients: cancelled private edits and undo keep every target; saves retain other targets`, async ({
    page,
  }) => {
    let state: any = {
      id: "7",
      merchant_id: "1",
      revision: "2",
      voice_order: false,
      email_masked: "o***@example.invalid",
      msgconfig: {
        order: 0,
        settle: 0,
        login: 0,
        complain: 0,
        mchrisk: 0,
        balance: 0,
        group: 0,
        domain_audit: 0,
        daily_report: 0,
        order_money: "10.00",
        balance_money: "20.00",
      },
      configured: { qqbot_appsecret: true, telegram_uid: true },
      target_masks: { qqbot_appsecret: "********", telegram_uid: "old***245" },
    };
    const path = area === "admin" ? "/merchants/1/notice" : "/account/notice";
    const requests = await api(page, area, (p, method, body) => {
      if (p !== path) return;
      if (method === "PUT") {
        expect(body.expected_profile_id).toBe(state.id);
        state = {
          ...state,
          id: String(Number(state.id) + 1),
          revision: String(Number(state.revision) + 1),
          msgconfig: body.msgconfig,
          voice_order: body.voice_order,
          configured: { ...state.configured },
          target_masks: { ...state.target_masks },
        };
        for (const key of Object.keys(body.targets)) {
          state.configured[key] = true;
          state.target_masks[key] = "new***245";
        }
        for (const key of body.clear_fields) {
          state.configured[key] = false;
          state.target_masks[key] = "";
        }
      }
      return state;
    });
    await page.goto(
      area === "admin" ? "/admin/merchant/notice" : "/merchant/account/notice",
    );
    if (area === "admin") {
      await page.getByLabel("商户 ID", { exact: true }).fill("1");
      await page
        .getByRole("button", { name: "读取商户设置", exact: true })
        .click();
    }
    const target = page.locator('[data-target-action="qqbot_appsecret"]'),
      field = page.locator('[data-target-value="qqbot_appsecret"]');
    await target
      .getByRole("button", { name: "修改QQ Bot AppSecret", exact: true })
      .click();
    await field.fill("discarded-notice245");
    await target
      .getByRole("button", { name: "取消修改QQ Bot AppSecret", exact: true })
      .click();
    await expect(field).toHaveCount(0);
    await target
      .getByRole("button", { name: "清除QQ Bot AppSecret", exact: true })
      .click();
    await target
      .getByRole("button", { name: "撤销清除QQ Bot AppSecret", exact: true })
      .click();
    expect(requests.writes).toHaveLength(0);
    await target
      .getByRole("button", { name: "修改QQ Bot AppSecret", exact: true })
      .click();
    await field.fill("new-notice-private245");
    await page.getByRole("button", { name: "保存设置", exact: true }).click();
    await expect(
      page.getByText("通知设置已保存", { exact: true }),
    ).toBeVisible();
    expect(requests.writes[0]).toEqual({
      path,
      method: "PUT",
      body: {
        expected_profile_id: "7",
        msgconfig: state.msgconfig,
        voice_order: false,
        targets: { qqbot_appsecret: "new-notice-private245" },
        clear_fields: [],
      },
    });
    await pageReload248(page);
    if (area === "admin") {
      await page.getByLabel("商户 ID", { exact: true }).fill("1");
      await page
        .getByRole("button", { name: "读取商户设置", exact: true })
        .click();
    }
    await expect(
      target.getByRole("button", { name: "修改QQ Bot AppSecret", exact: true }),
    ).toBeVisible();
    await target
      .getByRole("button", { name: "清除QQ Bot AppSecret", exact: true })
      .click();
    expect(requests.writes).toHaveLength(1);
    await page.getByRole("button", { name: "保存设置", exact: true }).click();
    await expect(
      target.getByRole("button", { name: "设置QQ Bot AppSecret", exact: true }),
    ).toBeVisible();
    expect(requests.writes[1].body).toEqual({
      expected_profile_id: "8",
      msgconfig: state.msgconfig,
      voice_order: false,
      targets: {},
      clear_fields: ["qqbot_appsecret"],
    });
    expect(state.configured.telegram_uid).toBe(true);
    expect(requests.writes).toHaveLength(2);
    await privateState(page, requests, [
      "discarded-notice245",
      "new-notice-private245",
    ]);
  });
}

for (const kind of ["certificate", "ocr"] as const) {
  test(`KYC ${kind}: required service guard, cancel, clear undo, exact shared CAS and readback`, async ({
    page,
  }) => {
    let state = kycConfig233();
    state.revision = "3";
    state.cert_open = 2;
    state.ocr_type = "aliyun";
    for (const entry of Object.values(state.credentials)) {
      entry.present = true;
      entry.masked = "old***245";
    }
    const path =
      kind === "certificate" ? "/settings/certification" : "/settings/ocr";
    const requests = await api(page, "admin", (p, method, body) => {
      if (p !== "/settings/certification" && p !== "/settings/ocr") return;
      if (method === "PUT") {
        expect(body.expected_revision).toBe(state.revision);
        const credentials = structuredClone(state.credentials);
        for (const [key, edit] of Object.entries(body.credentials) as [
          keyof typeof credentials,
          any,
        ][]) {
          if (edit.action === "replace")
            credentials[key] = { present: true, masked: "new***245" };
          else if (edit.action === "clear")
            credentials[key] = { present: false, masked: "" };
        }
        state = {
          ...state,
          ...body,
          revision: String(Number(state.revision) + 1),
          credentials,
        };
      }
      return state;
    });
    await page.goto("/admin/system/settings?tab=" + kind);
    const title = kind === "certificate" ? "实名认证配置" : "OCR文字识别配置",
      label =
        kind === "certificate"
          ? "手机号三要素AppCode"
          : "OCR阿里云AccessKey Secret",
      key = kind === "certificate" ? "cert_appcode" : "ocr_aliyunkey",
      panel = page.getByRole("region", { name: title, exact: true }),
      group = panel.getByRole("group", { name: label + "配置", exact: true }),
      field = panel.getByLabel(label, { exact: true }),
      save = panel.getByRole("button", {
        name: kind === "certificate" ? "保存实名认证配置" : "保存OCR配置",
        exact: true,
      });
    await expect(
      group.getByRole("button", { name: "清除" + label, exact: true }),
    ).toBeDisabled();
    await group
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await field.fill("discarded-kyc245");
    await group
      .getByRole("button", { name: "取消修改" + label, exact: true })
      .click();
    await expect(field).toHaveValue("");
    await group
      .getByRole("button", { name: "修改" + label, exact: true })
      .click();
    await field.fill("new-kyc-private245");
    await save.click();
    expect(requests.writes).toHaveLength(0);
    await confirm(page, "确认保存" + title);
    await expect(
      panel.getByText("配置已保存。", { exact: true }),
    ).toBeVisible();
    expect(requests.writes[0].path).toBe(path);
    expect(requests.writes[0].body.expected_revision).toBe("3");
    for (const [name, edit] of Object.entries(
      requests.writes[0].body.credentials,
    ))
      expect(edit).toEqual(
        name === key
          ? { action: "replace", value: "new-kyc-private245" }
          : { action: "keep", value: "" },
      );
    await pageReload248(page);
    await expect(
      group.getByRole("button", { name: "修改" + label, exact: true }),
    ).toBeVisible();
    await choose(
      panel,
      kind === "certificate" ? "是否开启实名认证" : "文字识别接口",
      "关闭",
    );
    await panel
      .getByRole("button", { name: "管理其它服务凭据项", exact: true })
      .click();
    await group
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await group
      .getByRole("button", { name: "撤销清除" + label, exact: true })
      .click();
    expect(requests.writes).toHaveLength(1);
    await group
      .getByRole("button", { name: "清除" + label, exact: true })
      .click();
    await save.click();
    await confirm(page, "确认保存" + title);
    await expect(
      group.getByRole("button", { name: "设置" + label, exact: true }),
    ).toBeVisible();
    expect(requests.writes[1].body.expected_revision).toBe("4");
    expect(requests.writes[1].body.credentials[key]).toEqual({
      action: "clear",
      value: "",
    });
    expect(requests.writes[1].body.cert_open).toBe(
      kind === "certificate" ? 0 : 2,
    );
    expect(requests.writes[1].body.ocr_type).toBe(
      kind === "ocr" ? "" : "aliyun",
    );
    expect(requests.writes).toHaveLength(2);
    await privateState(page, requests, [
      "discarded-kyc245",
      "new-kyc-private245",
    ]);
  });
}

test("parent channel public editing, private cancellation, undo and separate PEM namespace save with exact fingerprint", async ({
  page,
}) => {
  let state: any = {
    id: "9007199254740993",
    name: "Synthetic parent245",
    pay_type: "alipay",
    plugin: "alipay",
    configured: true,
    source_channel_id: "8",
    revision: "1",
    account_version: "1",
    enabled: false,
    archived: false,
    mode: 0,
    rate: "99.50",
    fee_rate_bps: 50,
    fingerprint: fp(1),
    account: {
      plugin: "alipay",
      app_types: ["1"],
      mode: 0,
      fields: [
        {
          name: "appid",
          namespace: "config",
          label: "应用ID",
          secret: false,
          pem: false,
          present: true,
          value: "original-app245",
        },
        {
          name: "appkey",
          namespace: "config",
          label: "应用密钥",
          secret: true,
          pem: false,
          present: true,
        },
        {
          name: "appkey",
          namespace: "certificates",
          label: "签名私钥",
          secret: true,
          pem: true,
          present: true,
        },
      ],
    },
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
  };
  const path = "/payment-channels/" + state.id;
  const requests = await api(page, "admin", (p, method, body) => {
    if (p === "/payment-channels/templates") return { items: [], total: 0 };
    if (p === "/payment-channels")
      return { items: [state], total: 1, page: 1, size: 20 };
    if (p !== path) return;
    if (method === "PATCH") {
      expect(body.expected_fingerprint).toBe(state.fingerprint);
      state = structuredClone(state);
      state.revision = String(Number(state.revision) + 1);
      state.account_version = state.revision;
      state.fingerprint = fp(Number(state.revision));
      for (const field of state.account.fields) {
        const edit = body.account[field.namespace][field.name];
        if (edit.operation === "clear") {
          field.present = false;
          delete field.value;
        } else if (edit.operation === "replace") {
          field.present = true;
          if (!field.secret) field.value = edit.value;
        }
      }
    }
    return state;
  });
  await page.goto("/admin/payapi/pay-channel");
  await page.locator("[data-parent-parameters]").click();
  const dialog = page.getByRole("dialog", { name: "父通道配置", exact: true });
  await expect(dialog.getByLabel("应用ID 内容", { exact: true })).toHaveValue(
    "original-app245",
  );
  await dialog
    .getByLabel("应用密钥 内容", { exact: true })
    .fill("discarded-parent245");
  await dialog
    .getByRole("button", { name: "取消修改应用密钥", exact: true })
    .click();
  await expect(dialog.getByLabel("应用密钥 内容", { exact: true })).toHaveValue(
    "",
  );
  await dialog
    .getByRole("button", { name: "清除应用密钥", exact: true })
    .click();
  await dialog
    .getByRole("button", { name: "撤销清除应用密钥", exact: true })
    .click();
  expect(requests.writes).toHaveLength(0);
  await dialog
    .getByLabel("应用ID 内容", { exact: true })
    .fill("new-parent-app245");
  const pem =
    "-----BEGIN PRIVATE KEY-----\nsynthetic-private245\n-----END PRIVATE KEY-----";
  await dialog.getByLabel("签名私钥 内容", { exact: true }).fill(pem);
  await dialog.locator("[data-parent-save]").click();
  await expect(dialog).toHaveCount(0);
  expect(requests.writes).toEqual([
    {
      path,
      method: "PATCH",
      body: {
        expected_fingerprint: fp(1),
        name: "Synthetic parent245",
        rate: "99.50",
        account: {
          app_types: ["1"],
          config: {
            appid: { operation: "replace", value: "new-parent-app245" },
            appkey: { operation: "keep" },
          },
          certificates: { appkey: { operation: "replace", value: pem } },
        },
      },
    },
  ]);
  await page.locator("[data-parent-parameters]").click();
  await expect(dialog.getByLabel("应用ID 内容", { exact: true })).toHaveValue(
    "new-parent-app245",
  );
  await expect(dialog.getByLabel("签名私钥 内容", { exact: true })).toHaveValue(
    "",
  );
  await dialog
    .getByRole("button", { name: "清除应用密钥", exact: true })
    .click();
  await dialog.locator("[data-parent-save]").click();
  await expect(dialog).toHaveCount(0);
  expect(requests.writes[1].body.expected_fingerprint).toBe(fp(2));
  expect(requests.writes[1].body.account.config.appkey).toEqual({
    operation: "clear",
  });
  expect(requests.writes[1].body.account.certificates.appkey).toEqual({
    operation: "keep",
  });
  expect(requests.writes).toHaveLength(2);
  await privateState(page, requests, ["discarded-parent245", pem]);
});

test("child parameters cancel write-only drafts, undo optional clear and require fresh required values on parent switch", async ({
  page,
}) => {
  const fixture = JSON.parse(
    fs.readFileSync(
      new URL("./subchannel168b-fixture.json", import.meta.url),
      "utf8",
    ),
  );
  let state = structuredClone(fixture.admin.items[0]);
  const schema = structuredClone(fixture.schemas.items[0]);
  schema.fields[1].required = false;
  const catalog = {
      items: [schema, { ...schema, parent_id: "8", fingerprint: fp(8) }],
      total: 2,
    },
    path = "/payment-subchannels/" + state.id;
  const requests = await api(page, "admin", (p, method, body) => {
    if (p === "/payment-subchannels/schemas") return catalog;
    if (p === "/payment-subchannels/policy") return fixture.policy;
    if (p === "/payment-subchannels")
      return { ...fixture.admin, items: [state], total: 1 };
    if (p !== path) return;
    if (method === "PATCH") {
      expect(body.expected_fingerprint).toBe(state.fingerprint);
      state = {
        ...state,
        parent_id: body.parent_id,
        name: body.name,
        revision: String(Number(state.revision) + 1),
        account_version: String(Number(state.account_version) + 1),
        fingerprint: fp(Number(state.revision) + 1),
        schema_fingerprint: catalog.items.find(
          (s) => s.parent_id === body.parent_id,
        )!.fingerprint,
        fields: state.fields.map((field: any) => ({
          ...field,
          present: body.values[field.name].operation === "clear" ? false : true,
        })),
      };
    }
    return state;
  });
  await page.goto("/admin/applyments/subchannel");
  async function open() {
    await page
      .getByRole("button", {
        name: "子通道 " + state.id + " 操作",
        exact: true,
      })
      .click();
    await page.getByRole("menuitem", { name: "参数设置", exact: true }).click();
  }
  await open();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("secret", { exact: true }).fill("discarded-child245");
  await dialog
    .getByRole("button", { name: "取消修改secret", exact: true })
    .click();
  await expect(dialog.getByLabel("secret", { exact: true })).toHaveValue("");
  await dialog.getByRole("button", { name: "清除secret", exact: true }).click();
  await dialog
    .getByRole("button", { name: "撤销清除secret", exact: true })
    .click();
  expect(requests.writes).toHaveLength(0);
  await dialog.getByLabel("app", { exact: true }).fill("new-child-app245");
  await dialog.getByRole("button", { name: "清除secret", exact: true }).click();
  const before = state.fingerprint;
  await dialog.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(requests.writes[0]).toEqual({
    path,
    method: "PATCH",
    body: {
      expected_fingerprint: before,
      name: state.name,
      parent_id: "7",
      values: {
        app: { operation: "replace", value: "new-child-app245" },
        secret: { operation: "clear" },
      },
    },
  });
  await open();
  await expect(
    dialog.getByRole("button", { name: "清除secret", exact: true }),
  ).toHaveCount(0);
  await dialog
    .getByLabel("secret", { exact: true })
    .fill("old-parent-child245");
  await choose(dialog, "父通道", "支付宝 #8");
  await expect(dialog.getByLabel("secret", { exact: true })).toHaveValue("");
  await dialog.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(
    dialog.getByText("请填写新的app", { exact: true }),
  ).toBeVisible();
  expect(requests.writes).toHaveLength(1);
  await dialog
    .getByLabel("app", { exact: true })
    .fill("new-parent-required245");
  await dialog.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(requests.writes[1].body).toMatchObject({
    expected_fingerprint: fp(2),
    parent_id: "8",
    values: {
      app: { operation: "replace", value: "new-parent-required245" },
      secret: { operation: "clear" },
    },
  });
  expect(requests.writes).toHaveLength(2);
  await privateState(page, requests, [
    "discarded-child245",
    "old-parent-child245",
  ]);
});
