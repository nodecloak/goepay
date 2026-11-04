import { test, expect, type Page } from "@playwright/test";

const providers = [
  {
    provider: "aliyun",
    title: "阿里云短信",
    principal: "AccessKeyId",
    secret: "AccessKeySecret",
    first: "access_key_id",
    second: "access_key_secret",
  },
  {
    provider: "qcloud",
    title: "腾讯云短信",
    principal: "AppId",
    secret: "AppKey",
    first: "appid",
    second: "appkey",
  },
  {
    provider: "smsbao",
    title: "短信宝",
    principal: "账号",
    secret: "密码",
    first: "user",
    second: "pass",
  },
] as const;
async function fixture(page: Page, tab: "site" | "mail" | "sms") {
  const captcha = { configured: true, fingerprint: "a".repeat(64) };
  const mail = {
    name: "smtp",
    title: "SMTP发信",
    status: 0,
    configured: true,
    config: {
      host: "smtp.synthetic245.invalid",
      port: "465",
      username: "mail245@example.invalid",
    },
    password_set: true,
    revision: "1",
    fingerprint: "a".repeat(64),
    test_available: false,
    effective_source: "console",
  };
  const sms = {
    active: "aliyun",
    status: 0,
    fingerprint: "a".repeat(64),
    providers: providers.map((p) => ({
      provider: p.provider,
      configured: true,
      template: `template-${p.provider}-245`,
      sign: "Synthetic245",
      principal: `${p.provider.slice(0, 2)}****45`,
    })),
  };
  const writes: { path: string; body: any }[] = [],
    forbidden: string[] = [];
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname,
      method = req.method();
    let data: unknown = {};
    if (/\/(?:test|tests)(?:\/|$)/.test(path)) {
      forbidden.push(path);
      return route.fulfill({
        status: 403,
        contentType: "application/json",
        body: JSON.stringify({
          code: -1,
          message: "Outbound testing is disabled in this fixture",
        }),
      });
    }
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "1",
          name: "Local credential245",
          role: "admin",
          admin_class: "super",
        },
        csrf_token: "synthetic245",
      };
    else if (path.endsWith("/resources/settings/schema"))
      data = {
        title: "设置",
        fields: [{ name: "key", type: "select", options: ["site_name"] }],
        permissions: { create: true, update: true, delete: false },
      };
    else if (path.endsWith("/resources/settings"))
      data = {
        items: [
          { id: "1", key: "site_name", value: "Synthetic credentials245" },
        ],
        total: 1,
      };
    else if (path.endsWith("/settings/captcha")) {
      if (method === "PATCH") {
        const body = req.postDataJSON();
        expect(body.expected_fingerprint).toBe(captcha.fingerprint);
        writes.push({ path, body });
        captcha.configured = body.action === "replace";
        captcha.fingerprint = "abcdef"[writes.length].repeat(64);
      }
      data = captcha;
    } else if (path.endsWith("/settings/mail-channels/smtp")) {
      if (method === "PATCH") {
        const body = req.postDataJSON();
        expect(body.expected_fingerprint).toBe(mail.fingerprint);
        writes.push({ path, body });
        mail.config = {
          host: body.config.host,
          port: body.config.port,
          username: body.config.username,
        };
        mail.status = body.status;
        mail.revision = String(Number(mail.revision) + 1);
        mail.fingerprint = "abcdef"[writes.length].repeat(64);
      }
      data = mail;
    } else if (path.endsWith("/settings/mail-frequency")) {
      const fields = {
        mail_interval_email: "60",
        mail_interval_ip: "60",
        mail_daily_email: "3",
        mail_daily_ip: "6",
      };
      data = {
        ...fields,
        effective: fields,
        configured: true,
        revision: "1",
        fingerprint: "a".repeat(64),
      };
    } else if (path.endsWith("/settings/sms-channels")) {
      if (method === "PATCH") {
        const body = req.postDataJSON();
        expect(body.expected_fingerprint).toBe(sms.fingerprint);
        writes.push({ path, body });
        const provider = sms.providers.find(
          (p) => p.provider === body.provider,
        )!;
        provider.template = body.credentials.template;
        provider.sign = body.credentials.sign;
        provider.principal = "ne****45";
        sms.active = body.active;
        sms.status = body.status;
        sms.fingerprint = "b".repeat(64);
      }
      data = sms;
    } else if (path.endsWith("/settings/business-sms"))
      data = {
        revision: "0",
        active_provider: "aliyun",
        ready: { aliyun: true, qcloud: true, smsbao: true },
        templates: Object.fromEntries(
          providers.flatMap((p) =>
            ["balance", "complain", "group"].map((scene) => [
              `${p.provider}_${scene}`,
              "",
            ]),
          ),
        ),
      };
    else if (method !== "GET") forbidden.push(path);
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto(`/admin/system/settings?tab=${tab}`);
  return { writes, forbidden, mail, sms };
}

test("CAPTCHA editing, returning from both confirmations and explicit save/readback never mutate on cancellation", async ({
  page,
}) => {
  const { writes, forbidden } = await fixture(page, "site");
  const panel = page.getByRole("region", {
    name: "极验验证码密钥",
    exact: true,
  });
  const input = panel.getByLabel("新的极验验证码密钥", { exact: true });
  await panel.getByRole("button", { name: "替换密钥", exact: true }).click();
  await input.fill("private-cancel-captcha245");
  await panel.getByRole("button", { name: "取消编辑", exact: true }).click();
  await expect(input).toHaveCount(0);
  expect(writes).toHaveLength(0);
  await panel.getByRole("button", { name: "替换密钥", exact: true }).click();
  await expect(input).toHaveValue("");
  await input.fill("synthetic-captcha-key245");
  await panel.getByRole("button", { name: "保存新密钥", exact: true }).click();
  await panel.getByRole("button", { name: "返回修改", exact: true }).click();
  await expect(input).toHaveValue("synthetic-captcha-key245");
  expect(writes).toHaveLength(0);
  await panel.getByRole("button", { name: "保存新密钥", exact: true }).click();
  await panel.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(panel.getByRole("status")).toHaveText("验证码密钥已保存");
  expect(writes[0].body).toEqual({
    action: "replace",
    key: "synthetic-captcha-key245",
    expected_fingerprint: "a".repeat(64),
  });
  await panel.getByRole("button", { name: "清除密钥", exact: true }).click();
  await panel.getByRole("button", { name: "返回修改", exact: true }).click();
  expect(writes).toHaveLength(1);
  await panel.getByRole("button", { name: "清除密钥", exact: true }).click();
  await panel.getByRole("button", { name: "确认清除", exact: true }).click();
  await expect(panel.getByRole("status")).toHaveText("验证码密钥已清除");
  expect(writes).toHaveLength(2);
  expect(writes[1].body).toEqual({
    action: "clear",
    expected_fingerprint: "b".repeat(64),
  });
  await page.reload();
  await expect(
    panel.getByRole("button", { name: "设置密钥", exact: true }),
  ).toBeVisible();
  expect(forbidden).toEqual([]);
});

test("SMTP cancels private input, preserves unchanged password, validates changed identity and reopens its own draft", async ({
  page,
}) => {
  const { writes, forbidden } = await fixture(page, "mail");
  const panel = page.getByRole("region", { name: "邮箱配置", exact: true });
  const dialog = page.getByRole("dialog", {
    name: "SMTP发信账号设置",
    exact: true,
  });
  await panel.getByRole("button", { name: "账号设置", exact: true }).click();
  await dialog
    .getByLabel("邮箱密码", { exact: true })
    .fill("private-cancel-mail245");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await panel.getByRole("button", { name: "账号设置", exact: true }).click();
  await expect(dialog.getByLabel("邮箱密码", { exact: true })).toHaveValue("");
  await dialog
    .getByLabel("SMTP服务器", { exact: true })
    .fill("changed.synthetic245.invalid");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "更换服务器、端口或账号时请重新填写邮箱密码",
  );
  expect(writes).toHaveLength(0);
  await dialog
    .getByLabel("SMTP服务器", { exact: true })
    .fill("smtp.synthetic245.invalid");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(writes[0].body).toEqual({
    name: "smtp",
    status: 0,
    config: {
      host: "smtp.synthetic245.invalid",
      port: "465",
      username: "mail245@example.invalid",
    },
    expected_fingerprint: "a".repeat(64),
    keep_password: true,
  });
  await panel.getByRole("button", { name: "账号设置", exact: true }).click();
  await dialog
    .getByLabel("SMTP服务器", { exact: true })
    .fill("new.synthetic245.invalid");
  await dialog.getByLabel("SMTP端口", { exact: true }).fill("587");
  await dialog
    .getByLabel("邮箱密码", { exact: true })
    .fill("synthetic-mail-password245");
  await dialog
    .getByRole("button", { name: "收起并保留草稿", exact: true })
    .click();
  await panel
    .getByRole("button", { name: "继续编辑草稿", exact: true })
    .click();
  await expect(dialog.getByLabel("SMTP服务器", { exact: true })).toHaveValue(
    "new.synthetic245.invalid",
  );
  await expect(dialog.getByLabel("邮箱密码", { exact: true })).toHaveValue(
    "synthetic-mail-password245",
  );
  expect(writes).toHaveLength(1);
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(writes[1].body).toEqual({
    name: "smtp",
    status: 0,
    config: {
      host: "new.synthetic245.invalid",
      port: "587",
      username: "mail245@example.invalid",
      password: "synthetic-mail-password245",
    },
    expected_fingerprint: "b".repeat(64),
  });
  await page.reload();
  await panel.getByRole("button", { name: "账号设置", exact: true }).click();
  await expect(dialog.getByLabel("SMTP服务器", { exact: true })).toHaveValue(
    "new.synthetic245.invalid",
  );
  await expect(dialog.getByLabel("邮箱密码", { exact: true })).toHaveValue("");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(writes).toHaveLength(2);
  expect(forbidden).toEqual([]);
});

for (const provider of providers) {
  test(`${provider.title} never submits stored masks and keeps other gateways intact through cancel, draft resume and explicit replacement`, async ({
    page,
  }) => {
    const { writes, forbidden, sms } = await fixture(page, "sms");
    const panel = page.getByRole("region", { name: "短信配置", exact: true });
    const gatewayTable = panel.getByRole("table").filter({
      has: page.getByRole("columnheader", { name: "账号", exact: true }),
    });
    const row = gatewayTable.getByRole("row").filter({
      has: page.getByRole("cell", { name: provider.title, exact: true }),
    });
    const dialog = page.getByRole("dialog", {
      name: `${provider.title}设置`,
      exact: true,
    });
    await expect(gatewayTable).toHaveCount(1);
    await expect(row).toHaveCount(1);
    await row.getByRole("button", { name: "服务商设置", exact: true }).click();
    await expect(
      dialog.getByLabel(provider.principal, { exact: true }),
    ).toHaveValue("");
    await expect(
      dialog.getByLabel(provider.secret, { exact: true }),
    ).toHaveValue("");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText(
      "请填写完整的服务商密钥",
    );
    expect(writes).toHaveLength(0);
    await dialog
      .getByLabel(provider.secret, { exact: true })
      .fill("private-cancel-sms245");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await row.getByRole("button", { name: "服务商设置", exact: true }).click();
    await expect(
      dialog.getByLabel(provider.secret, { exact: true }),
    ).toHaveValue("");
    await dialog
      .getByLabel(provider.principal, { exact: true })
      .fill(`new-${provider.provider}-id245`);
    await dialog
      .getByLabel(provider.secret, { exact: true })
      .fill(`synthetic-${provider.provider}-secret245`);
    await dialog.getByLabel("模板", { exact: true }).fill("new-template245");
    await dialog
      .getByRole("button", { name: "收起并保留草稿", exact: true })
      .click();
    await row
      .getByRole("button", { name: "继续编辑草稿", exact: true })
      .click();
    await expect(
      dialog.getByLabel(provider.secret, { exact: true }),
    ).toHaveValue(`synthetic-${provider.provider}-secret245`);
    expect(writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(writes).toHaveLength(1);
    expect(writes[0].body).toEqual({
      provider: provider.provider,
      credentials: {
        template: "new-template245",
        sign: "Synthetic245",
        [provider.first]: `new-${provider.provider}-id245`,
        [provider.second]: `synthetic-${provider.provider}-secret245`,
      },
      active: "aliyun",
      status: 0,
      expected_fingerprint: "a".repeat(64),
    });
    for (const other of sms.providers.filter(
      (p) => p.provider !== provider.provider,
    )) {
      expect(other.template).toBe(`template-${other.provider}-245`);
      expect(other.principal).not.toBe("ne****45");
    }
    await page.reload();
    await expect(row).toContainText("new-template245");
    await row.getByRole("button", { name: "服务商设置", exact: true }).click();
    await expect(
      dialog.getByLabel(provider.principal, { exact: true }),
    ).toHaveValue("");
    await expect(
      dialog.getByLabel(provider.secret, { exact: true }),
    ).toHaveValue("");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    expect(forbidden).toEqual([]);
  });
}
