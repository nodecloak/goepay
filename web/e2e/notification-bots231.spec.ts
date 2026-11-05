import { pageReload248 } from "./fixtures/page-reload248";
import { test, expect, type Page, type Locator } from "@playwright/test";

const blank = () => ({
  revision: "0",
  enabled: false,
  app_id_masked: "",
  secret_present: false,
  bound: false,
  recipient_masked: "",
  phone_masked: "",
  token_source: "",
});
async function fixture(
  page: Page,
  options: { qq?: boolean; unknownSave?: boolean } = {},
) {
  const states: Record<string, ReturnType<typeof blank>> = {
    robot: blank(),
    telegram: blank(),
    qqbot: {
      ...blank(),
      revision: options.qq ? "1" : "0",
      enabled: !!options.qq,
      secret_present: !!options.qq,
      app_id_masked: options.qq ? "12***78" : "",
    },
  };
  const writes: {
    path: string;
    method: string;
    body: Record<string, unknown> | null;
  }[] = [];
  let pollBound = false;
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname,
      method = req.method();
    let data: unknown = {};
    if (path.endsWith("/session"))
      data = {
        user: { id: "1", role: "admin", admin_class: "super", name: "Bots231" },
        csrf_token: "synthetic231",
      };
    else if (path.endsWith("/resources/settings/schema"))
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
              "msgtelegram_chatid",
              "qqbot_openid",
            ],
          },
        ],
        permissions: { create: true, update: true, delete: false },
      };
    else if (path.endsWith("/resources/settings"))
      data = {
        items: [{ id: "1", key: "site_name", value: "本地消息配置231" }],
        total: 1,
      };
    else if (
      /\/settings\/notice-accounts\/(robot|telegram|qqbot)$/.test(path)
    ) {
      const kind = path.split("/").at(-1)!;
      if (method === "PUT") {
        const body = req.postDataJSON();
        writes.push({ path, method, body });
        if (options.unknownSave) return route.abort("failed");
        states[kind] = {
          ...states[kind],
          revision: String(Number(states[kind].revision) + 1),
          enabled: body.enabled,
          secret_present:
            body.credential_action === "clear"
              ? false
              : body.credential_action === "replace"
                ? kind !== "robot" || !!body.app_id
                : states[kind].secret_present,
          phone_masked: body.phone ? "138***001" : "",
          app_id_masked:
            body.credential_action === "replace"
              ? "synt***231"
              : states[kind].app_id_masked,
          bound: false,
          recipient_masked: "",
          token_source: body.token_source || states[kind].token_source,
        };
      } else if (kind === "qqbot" && pollBound)
        states.qqbot = {
          ...states.qqbot,
          revision: "2",
          bound: true,
          recipient_masked: "op***id",
        };
      data = states[kind];
    } else if (path.endsWith("/notice-accounts/qqbot/binding")) {
      writes.push({ path, method, body: req.postDataJSON() });
      if (method === "POST")
        data = {
          id: "a".repeat(48),
          challenge: "GOEPAY-0123456789abcdefabcd",
          expires_at: new Date(Date.now() + 300000).toISOString(),
        };
      else {
        pollBound = false;
        states.qqbot = {
          ...states.qqbot,
          revision: "3",
          bound: false,
          recipient_masked: "",
        };
        data = states.qqbot;
      }
    } else if (path.includes("/notice-accounts/qqbot/binding/")) {
      writes.push({ path, method, body: null });
      data = { cancelled: true };
    } else if (
      path.endsWith("/settings/voice") ||
      path.endsWith("/printing/settings")
    )
      data = { ...blank(), provider: "excelsecu" };
    else if (path.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (path.endsWith("/dashboard"))
      data = {
        orders: 0,
        paid_orders: 0,
        summary: {},
        trend: [],
        payment_mix: [],
        announcements: [],
        recent_settlements: [],
      };
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/system/settings");
  await page.getByRole("tab", { name: "消息提醒", exact: true }).click();
  await expect(
    page
      .getByRole("region", { name: "管理员QQ机器人配置", exact: true })
      .getByRole("button", { name: "保存管理员QQ机器人配置", exact: true }),
  ).toBeVisible();
  return {
    writes,
    bind: () => {
      pollBound = true;
    },
  };
}
async function choose(section: Locator, label: string, choice: string) {
  await section.getByRole("button", { name: label, exact: true }).click();
  await section.getByRole("option", { name: choice, exact: true }).click();
}

test("administrator editors save complete robot and OAuth Telegram settings without starting QQ binding", async ({
  page,
}) => {
  const { writes } = await fixture(page);
  const robot = page.getByRole("region", {
    name: "管理员群机器人配置",
    exact: true,
  });
  await expect(
    robot.getByLabel("管理员手机号码", { exact: true }),
  ).toBeVisible();
  await robot
    .getByRole("button", { name: "设置管理员群机器人配置凭据", exact: true })
    .click();
  await robot
    .getByLabel("群机器人Webhook地址", { exact: true })
    .fill("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic231");
  await robot.getByLabel("管理员手机号码", { exact: true }).fill("13800000001");
  await robot.getByRole("switch").click();
  await robot
    .getByRole("button", { name: "保存管理员群机器人配置", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "确认保存管理员群机器人配置" })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(robot.getByText("配置已保存。", { exact: true })).toBeVisible();
  expect(writes[0].body).toEqual({
    expected_revision: "0",
    enabled: true,
    credential_action: "replace",
    app_id: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic231",
    secret: "",
    phone: "13800000001",
    token_source: "",
  });
  const tg = page.getByRole("region", {
    name: "管理员Telegram配置",
    exact: true,
  });
  await tg
    .getByRole("button", { name: "设置管理员Telegram配置凭据", exact: true })
    .click();
  await tg
    .getByLabel("管理员Telegram Chat Id", { exact: true })
    .fill("-100123456789");
  await choose(tg, "Telegram Token来源", "复用快捷登录Telegram Token");
  await tg.getByRole("switch").click();
  await tg
    .getByRole("button", { name: "保存管理员Telegram配置", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "确认保存管理员Telegram配置" })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(tg.getByText("配置已保存。", { exact: true })).toBeVisible();
  expect(writes).toHaveLength(2);
  expect(writes[1].body).toEqual({
    expected_revision: "0",
    enabled: true,
    credential_action: "replace",
    app_id: "-100123456789",
    secret: "",
    phone: "",
    token_source: "oauth",
  });
  await expect(
    page.getByLabel("管理员QQ机器人OpenID", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByLabel("管理员Telegram会话ID", { exact: true }),
  ).toHaveCount(0);
  await page.setViewportSize({ width: 375, height: 812 });
  await expect(
    robot.getByLabel("管理员手机号码", { exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    ),
  ).toBe(true);
  await page.evaluate(async () => {
    window.scrollTo(0, 0);
    await Promise.all(
      document.getAnimations().map((a) => a.finished.catch(() => undefined)),
    );
  });
  await page.screenshot({
    path: "../.tools/bots231/notice-bots-mobile.png",
    fullPage: true,
  });
});

test("QQ challenge stays in memory, readback completes binding, and unbind requires confirmation", async ({
  page,
}) => {
  const { writes, bind } = await fixture(page, { qq: true });
  const qq = page.getByRole("region", {
    name: "管理员QQ机器人配置",
    exact: true,
  });
  await expect(
    qq.getByLabel("QQ机器人Webhook URL", { exact: true }),
  ).toHaveValue(new URL(page.url()).origin + "/payment/qqbot/webhook");
  expect(writes).toHaveLength(0);
  await qq.getByRole("button", { name: "开始QQ绑定", exact: true }).click();
  await expect(
    qq.getByText("GOEPAY-0123456789abcdefabcd", { exact: true }),
  ).toBeVisible();
  expect(writes[0]).toMatchObject({
    method: "POST",
    body: { expected_revision: "1" },
  });
  expect(page.url()).not.toContain("GOEPAY-");
  expect(
    await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ),
  ).not.toContain("GOEPAY-");
  bind();
  await expect(qq.getByText("QQ机器人已绑定。", { exact: true })).toBeVisible({
    timeout: 6000,
  });
  await expect(
    qq.getByText("GOEPAY-0123456789abcdefabcd", { exact: true }),
  ).toHaveCount(0);
  await qq.getByRole("button", { name: "解除QQ绑定", exact: true }).click();
  expect(writes).toHaveLength(1);
  await page
    .getByRole("dialog", { name: "确认解除QQ绑定" })
    .getByRole("button", { name: "确认解绑", exact: true })
    .click();
  await expect(qq.getByText("QQ绑定已解除。", { exact: true })).toBeVisible();
  expect(writes[1]).toMatchObject({
    method: "DELETE",
    body: { expected_revision: "2" },
  });
});

test("a lost save response clears secrets and permits readback without replaying the write", async ({
  page,
}) => {
  const { writes } = await fixture(page, { unknownSave: true });
  const qq = page.getByRole("region", {
    name: "管理员QQ机器人配置",
    exact: true,
  });
  await qq
    .getByRole("button", { name: "设置管理员QQ机器人配置凭据", exact: true })
    .click();
  await qq.getByLabel("管理员QQ机器人AppID", { exact: true }).fill("12345678");
  await qq
    .getByLabel("管理员QQ机器人AppSecret", { exact: true })
    .fill("synthetic-private231");
  await qq
    .getByRole("button", { name: "保存管理员QQ机器人配置", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "确认保存管理员QQ机器人配置" })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(qq.getByRole("alert")).toContainText("不会自动重试");
  await expect(
    qq.getByLabel("管理员QQ机器人AppSecret", { exact: true }),
  ).toHaveValue("");
  await expect(
    qq.getByRole("button", { name: "保存管理员QQ机器人配置", exact: true }),
  ).toBeDisabled();
  expect(writes).toHaveLength(1);
  await pageReload248(page);
  await expect(qq.getByRole("alert")).toHaveCount(0);
  expect(writes).toHaveLength(1);
});

test("a phone-only SMS destination and explicit QQ window cancellation use their own actions", async ({
  page,
}) => {
  const { writes } = await fixture(page, { qq: true });
  const robot = page.getByRole("region", {
    name: "管理员群机器人配置",
    exact: true,
  });
  await robot
    .getByRole("button", { name: "设置管理员群机器人配置凭据", exact: true })
    .click();
  await robot.getByLabel("管理员手机号码", { exact: true }).fill("13800000001");
  await robot
    .getByRole("button", { name: "保存管理员群机器人配置", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "确认保存管理员群机器人配置" })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(robot.getByText(/管理员手机已保存/)).toBeVisible();
  expect(writes[0].body).toEqual({
    expected_revision: "0",
    enabled: false,
    credential_action: "replace",
    app_id: "",
    secret: "",
    phone: "13800000001",
    token_source: "",
  });
  const qq = page.getByRole("region", {
    name: "管理员QQ机器人配置",
    exact: true,
  });
  await qq.getByRole("button", { name: "开始QQ绑定", exact: true }).click();
  await expect(
    qq.getByText("GOEPAY-0123456789abcdefabcd", { exact: true }),
  ).toBeVisible();
  await qq.getByRole("button", { name: "取消本次绑定", exact: true }).click();
  await expect(
    qq.getByText("本次绑定窗口已取消。", { exact: true }),
  ).toBeVisible();
  await expect(
    qq.getByText("GOEPAY-0123456789abcdefabcd", { exact: true }),
  ).toHaveCount(0);
  expect(writes[2]).toEqual({
    path:
      "/console/v1/admin/settings/notice-accounts/qqbot/binding/" +
      "a".repeat(48),
    method: "DELETE",
    body: null,
  });
});
