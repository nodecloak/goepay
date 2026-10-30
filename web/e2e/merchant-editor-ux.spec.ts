import { test, expect, type Page } from "@playwright/test";
async function fixture(page: Page) {
  const writes: { path: string; body: Record<string, unknown> }[] = [],
    reads: URL[] = [];
  const record = {
    id: "11901",
    name: "本地联系人商户",
    phone: "13000011901",
    email: "contact@example.invalid",
    qq: "1000011901",
    url: "shop.example.invalid",
    created_at: "2026-10-25T00:00:00Z",
    signature_algorithm: "MD5",
    enabled: true,
    settlement_recipient: {
      state: "configured",
      config_id: "42",
      type: "alipay",
      app_id: "",
      account_masked: "***1234",
      name_masked: "***",
    },
  };
  const fields = [
    {
      name: "id",
      label: "商户 ID",
      type: "string",
      required: true,
      create_only: true,
    },
    { name: "name", label: "名称", type: "string", required: true },
    { name: "phone", label: "手机号码", type: "string" },
    { name: "email", label: "电子邮箱", type: "email" },
    { name: "qq", label: "QQ", type: "string" },
    { name: "url", label: "网站域名", type: "string" },
    { name: "api_key", label: "API 密钥", type: "password", write_only: true },
    {
      name: "signature_algorithm",
      label: "签名算法",
      type: "select",
      options: [
        { value: "MD5", label: "MD5" },
        { value: "RSA", label: "RSA" },
      ],
      default: "MD5",
    },
    { name: "enabled", label: "启用", type: "boolean", default: true },
    { name: "created_at", label: "注册时间", type: "string", read_only: true },
    {
      name: "settlement_recipient",
      label: "结算账号与姓名（已脱敏）",
      type: "json",
      read_only: true,
    },
  ];
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      p = u.pathname;
    let data: unknown = { items: [], total: 0 };
    if (req.method() !== "GET")
      writes.push({ path: p, body: req.postDataJSON() });
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "Local", role: "admin", admin_class: "super" },
        csrf_token: "merchant119-csrf",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/merchants/schema"))
      data = {
        title: "商户管理",
        permissions: { create: true, update: true, delete: false },
        fields,
      };
    else if (p.endsWith("/settlement-recipient")) {
      if (req.method() === "GET") reads.push(u);
      else
        record.settlement_recipient = {
          ...record.settlement_recipient,
          config_id: "43",
          account_masked: "***4321",
        };
      data = record.settlement_recipient;
    } else if (p.endsWith("/merchants/11901")) {
      if (req.method() === "PATCH") Object.assign(record, req.postDataJSON());
      data = record;
    } else if (p.endsWith("/merchants")) {
      if (req.method() === "GET") reads.push(u);
      data = { items: [record], total: 1 };
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/merchant/merchant-list");
  await expect(
    page.getByRole("button", { name: "编辑", exact: true }),
  ).toBeVisible();
  return { writes, reads, record };
}
for (const width of [1280, 375]) {
  test(`merchant original contact cells and basic/key editor retain drafts at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await expect(
      page.getByRole("columnheader", { name: "联系方式", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell").filter({ hasText: "手机：13000011901" }),
    ).toContainText("QQ：1000011901");
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
        name: "编辑商户管理",
        exact: true,
      }),
      basic = dialog.getByRole("tab", { name: "基本信息", exact: true }),
      key = dialog.getByRole("tab", { name: "密钥信息", exact: true });
    await expect(basic).toHaveAttribute("aria-selected", "true");
    await expect(dialog.getByLabel("API 密钥", { exact: true })).toBeHidden();
    await dialog.getByLabel("手机号码", { exact: true }).fill("13000011902");
    await key.click();
    await expect(dialog.getByLabel("API 密钥", { exact: true })).toHaveValue(
      "",
    );
    await key.press("ArrowLeft");
    await expect(basic).toBeFocused();
    await expect(dialog.getByLabel("手机号码", { exact: true })).toHaveValue(
      "13000011902",
    );
    await dialog.getByLabel("QQ", { exact: true }).fill("");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]!.body).toMatchObject({
      phone: "13000011902",
      qq: "",
      url: "shop.example.invalid",
    });
    expect(f.writes[0]!.body).not.toHaveProperty("api_key");
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    await key.click();
    await dialog
      .getByLabel("API 密钥", { exact: true })
      .fill("discarded-synthetic-key-draft-119");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(basic).toHaveAttribute("aria-selected", "true");
    await key.click();
    await expect(dialog.getByLabel("API 密钥", { exact: true })).toHaveValue(
      "",
    );
    const bounds = await dialog.boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    expect(f.writes).toHaveLength(1);
  });
  test(`merchant create reveals missing API field before submitting at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await page.getByRole("button", { name: "新增商户", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "新增商户管理",
      exact: true,
    });
    await dialog.getByLabel("商户 ID", { exact: false }).fill("11902");
    await dialog.getByLabel("名称", { exact: false }).fill("本地新增商户");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(
      dialog.getByRole("tab", { name: "密钥信息", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await expect(dialog.getByLabel("API 密钥", { exact: false })).toBeFocused();
    expect(f.writes).toHaveLength(0);
    await dialog
      .getByLabel("API 密钥", { exact: false })
      .fill("synthetic-created-merchant-key-11902");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("新增已保存", { exact: true })).toBeVisible();
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]!.body.id).toBe("11902");
  });
  test(`merchant settlement edit uses its original owner and separate version at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "编辑商户管理",
      exact: true,
    });
    await dialog
      .getByRole("button", { name: "修改结算收款资料", exact: true })
      .click();
    const panel = dialog.getByRole("group", {
      name: "结算收款资料",
      exact: true,
    });
    await expect(
      panel.getByTestId("settlement-recipient-current"),
    ).toContainText("版本 42");
    expect(f.reads.at(-1)!.searchParams.get("merchant_id")).toBe("11901");
    expect(f.writes).toHaveLength(0);
    await panel
      .getByRole("button", { name: "修改收款资料", exact: true })
      .click();
    await panel
      .getByLabel("收款账号", { exact: true })
      .fill("local119@example.invalid");
    await panel.getByLabel("真实姓名", { exact: true }).fill("本地收款人");
    await panel
      .getByRole("button", { name: "保存收款版本", exact: true })
      .click();
    await expect(
      panel.getByTestId("settlement-recipient-current"),
    ).toContainText("版本 43");
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0]!.path).toBe(
      "/console/v1/admin/workflows/settlement-recipient",
    );
    expect(f.writes[0]!.body).toMatchObject({
      merchant_id: "11901",
      expected_config_id: "42",
    });
  });
}
