import { test, expect, type Page } from "@playwright/test";

const rows = [
  { kind: "oauth", field: "qq", flag: "qq_set", title: "QQ互联 APPKEY" },
  {
    kind: "oauth",
    field: "wx",
    flag: "wx_set",
    title: "微信开放平台 AppSecret",
  },
  {
    kind: "oauth",
    field: "alipay",
    flag: "alipay_set",
    title: "支付宝开放平台应用密钥",
  },
  {
    kind: "oauth",
    field: "telegram",
    flag: "telegram_set",
    title: "Telegram Bot Token",
  },
  { kind: "proxy", field: "password", flag: "password_set", title: "代理密码" },
  { kind: "proxy", field: "apikey", flag: "apikey_set", title: "API接口密钥" },
] as const;
type CredentialRow = (typeof rows)[number];
async function fixture(page: Page, row: CredentialRow, unknown = false) {
  const metadata: Record<string, unknown> =
    row.kind === "oauth"
      ? {
          qq_set: true,
          wx_set: true,
          alipay_set: true,
          telegram_set: true,
          fingerprint: "a".repeat(64),
        }
      : { password_set: true, apikey_set: true, fingerprint: "a".repeat(64) };
  const writes: Record<string, unknown>[] = [];
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    let data: unknown = {};
    if (path.endsWith("/session"))
      data = {
        user: {
          id: "1",
          name: "Local credentials245",
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
          {
            id: "1",
            key: "site_name",
            value: "Synthetic credential interactions245",
          },
        ],
        total: 1,
      };
    else if (path.endsWith(`/settings/${row.kind}-credentials`)) {
      if (req.method() === "PATCH") {
        const body = req.postDataJSON();
        writes.push(body);
        if (unknown) return route.abort("failed");
        expect(body.expected_fingerprint).toBe(metadata.fingerprint);
        const clearing =
          row.kind === "oauth"
            ? body.clear === true
            : body[`clear_${row.field}`] === true;
        if (row.kind === "oauth") expect(body.field).toBe(row.field);
        metadata[row.flag] = !clearing;
        metadata.fingerprint = "abcdef"[writes.length].repeat(64);
      }
      data = metadata;
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto(`/admin/system/settings?tab=${row.kind}`);
  const panel = page.getByRole("region", {
    name: row.kind === "oauth" ? "快捷登录密钥" : "中转代理密钥",
    exact: true,
  });
  await expect(panel).toBeVisible();
  return { panel, writes, metadata };
}

for (const row of rows) {
  test(`${row.title}: cancel, replace, clear, undo, explicit save and readback preserve other credentials`, async ({
    page,
  }) => {
    const { panel, writes } = await fixture(page, row);
    const control = panel.getByRole("group", {
      name: `${row.title}配置`,
      exact: true,
    });
    const input = panel.getByLabel(`新的${row.title}`, { exact: true });
    await control
      .getByRole("button", { name: `修改${row.title}`, exact: true })
      .click();
    await input.fill(`private-cancel-${row.field}-245`);
    await control
      .getByRole("button", { name: `取消修改${row.title}`, exact: true })
      .click();
    await expect(input).toHaveCount(0);
    expect(writes).toHaveLength(0);
    await control
      .getByRole("button", { name: `修改${row.title}`, exact: true })
      .click();
    await expect(input).toHaveValue("");
    await input.fill(`synthetic-${row.field}-245`);
    await panel
      .getByRole("button", { name: `保存${row.title}`, exact: true })
      .click();
    await expect(panel.getByRole("status")).toHaveText(`${row.title}已保存`);
    expect(writes).toEqual([
      row.kind === "oauth"
        ? {
            field: row.field,
            expected_fingerprint: "a".repeat(64),
            value: `synthetic-${row.field}-245`,
          }
        : {
            [row.field]: `synthetic-${row.field}-245`,
            expected_fingerprint: "a".repeat(64),
          },
    ]);
    await page.reload();
    await expect(control).toContainText("已配置");
    await control
      .getByRole("button", { name: `清除${row.title}`, exact: true })
      .click();
    await expect(control).toContainText("保存后清除");
    expect(writes).toHaveLength(1);
    await control
      .getByRole("button", { name: `撤销清除${row.title}`, exact: true })
      .click();
    await expect(control).not.toContainText("保存后清除");
    expect(writes).toHaveLength(1);
    await control
      .getByRole("button", { name: `清除${row.title}`, exact: true })
      .click();
    await panel
      .getByRole("button", { name: `保存${row.title}`, exact: true })
      .click();
    await expect(panel.getByRole("status")).toHaveText(`${row.title}已清除`);
    expect(writes).toHaveLength(2);
    expect(writes[1]).toEqual(
      row.kind === "oauth"
        ? {
            field: row.field,
            expected_fingerprint: "b".repeat(64),
            clear: true,
          }
        : {
            expected_fingerprint: "b".repeat(64),
            [`clear_${row.field}`]: true,
          },
    );
    await page.reload();
    await expect(control).toContainText("未配置");
    for (const other of rows.filter(
      (r) => r.kind === row.kind && r.field !== row.field,
    ))
      await expect(
        panel.getByRole("group", { name: `${other.title}配置`, exact: true }),
      ).toContainText("已配置");
    const stored = await page.evaluate(() =>
      JSON.stringify({
        local: { ...localStorage },
        session: { ...sessionStorage },
      }),
    );
    expect(stored).not.toContain(`synthetic-${row.field}-245`);
    expect(stored).not.toContain(`private-cancel-${row.field}-245`);
  });
}

for (const row of [rows[0], rows[4]]) {
  test(`${row.kind} clear with unknown response blocks blind replay`, async ({
    page,
  }) => {
    const { panel, writes } = await fixture(page, row, true);
    await panel
      .getByRole("button", { name: `清除${row.title}`, exact: true })
      .click();
    expect(writes).toHaveLength(0);
    await panel
      .getByRole("button", { name: `保存${row.title}`, exact: true })
      .click();
    await expect(panel.getByRole("alert")).toContainText("不会自动重试");
    expect(writes).toHaveLength(1);
    await expect(
      panel.getByRole("button", { name: `保存${row.title}`, exact: true }),
    ).toHaveCount(0);
    await expect(panel.getByRole("group")).toHaveCount(0);
  });
}
