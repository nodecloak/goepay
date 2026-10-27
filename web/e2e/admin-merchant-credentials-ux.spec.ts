import { test, expect } from "@playwright/test";
const secret = "16216216216216216216216216216216";
const privateKey =
  "-----BEGIN PRIVATE KEY-----\nU1lOVEhFVElDMTYy\n-----END PRIVATE KEY-----\n";
const publicKey =
  "-----BEGIN PUBLIC KEY-----\nU1lOVEhFVElDMTYy\n-----END PUBLIC KEY-----\n";
for (const width of [1280, 375])
  test(`original admin merchant MD5/RSA controls keep exact target and clear secrets at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const id = "9007199254740993",
      writes: { path: string; body: Record<string, unknown> }[] = [];
    let fingerprint = "a".repeat(64),
      revision = "1",
      currentPublic = "";
    const record = { id, name: "本地密钥商户", signature_algorithm: "MD5" };
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        p = new URL(req.url()).pathname;
      let data: unknown = { items: [], total: 0 };
      if (req.method() !== "GET")
        writes.push({ path: p, body: req.postDataJSON() });
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "credentials162-local",
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
          fields: [
            { name: "name", label: "名称", type: "string", required: true },
            {
              name: "api_key",
              label: "API 密钥",
              type: "password",
              write_only: true,
            },
            {
              name: "rsa_public_key",
              label: "商户 RSA 公钥",
              type: "text",
              write_only: true,
            },
            {
              name: "signature_algorithm",
              label: "签名算法",
              type: "select",
              options: [
                { value: "MD5", label: "MD5" },
                { value: "RSA", label: "RSA" },
              ],
            },
          ],
        };
      else if (p.endsWith(`/merchants/${id}/credentials/reset-md5`)) {
        expect(req.postDataJSON()).toEqual({
          expected_fingerprint: "a".repeat(64),
        });
        fingerprint = "b".repeat(64);
        revision = "2";
        data = {
          merchant_id: id,
          revision,
          algorithm: "MD5",
          fingerprint,
          api_key: secret,
        };
      } else if (p.endsWith(`/merchants/${id}/credentials/generate-rsa`)) {
        expect(req.postDataJSON()).toEqual({
          expected_fingerprint: "b".repeat(64),
        });
        fingerprint = "c".repeat(64);
        revision = "3";
        currentPublic = publicKey;
        data = {
          merchant_id: id,
          revision,
          algorithm: "MD5",
          fingerprint,
          public_key: publicKey,
          private_key: privateKey,
        };
      } else if (p.endsWith(`/merchants/${id}/credentials`))
        data = {
          merchant_id: id,
          revision,
          algorithm: "MD5",
          fingerprint,
          public_key: currentPublic,
          md5_configured: true,
        };
      else if (p.endsWith(`/merchants/${id}`)) {
        if (req.method() === "PATCH") Object.assign(record, req.postDataJSON());
        data = record;
      } else if (p.endsWith("/resources/merchants"))
        data = { items: [record], total: 1 };
      return route.fulfill({ json: { code: 0, data } });
    });
    await page.goto("/admin/merchant/merchant-list");
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const dialog = page.getByRole("dialog", {
      name: "编辑商户管理",
      exact: true,
    });
    await dialog
      .getByRole("textbox", { name: "名称", exact: false })
      .fill("保留普通资料草稿");
    await dialog.getByRole("tab", { name: "密钥信息", exact: true }).click();
    const panel = dialog.getByRole("region", {
      name: "商户密钥操作",
      exact: true,
    });
    await expect(panel).toContainText(id);
    const reset = panel.getByRole("button", {
      name: "重置MD5密钥",
      exact: true,
    });
    await expect(reset).toBeEnabled();
    await dialog
      .getByLabel("API 密钥", { exact: true })
      .fill("discarded-unsaved-secret162");
    await reset.click();
    const confirm = panel.getByRole("group", {
      name: "确认重置MD5密钥",
      exact: true,
    });
    await expect(confirm).toContainText(id);
    expect(writes).toHaveLength(0);
    await confirm.getByRole("button", { name: "取消", exact: true }).click();
    await expect(reset).toBeFocused();
    expect(writes).toHaveLength(0);
    await dialog.screenshot({
      path: test.info().outputPath(`admin-merchant-credentials-${width}.png`),
    });
    await reset.click();
    await confirm
      .getByRole("button", { name: "确定重置", exact: true })
      .click();
    await expect(
      panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
    ).toHaveValue(secret);
    await expect(dialog.getByLabel("API 密钥", { exact: true })).toHaveValue(
      "",
    );
    await expect(
      panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
    ).toBeFocused();
    await panel
      .getByLabel("新MD5密钥（仅本次显示）", { exact: true })
      .press("Enter");
    await expect(dialog).toBeVisible();
    expect(writes).toHaveLength(1);
    await panel
      .getByRole("button", { name: "关闭密钥显示", exact: true })
      .click();
    await expect(
      panel.getByLabel("新MD5密钥（仅本次显示）", { exact: true }),
    ).toHaveCount(0);
    await expect(reset).toBeFocused();
    await panel
      .getByRole("button", { name: "生成商户RSA密钥对", exact: true })
      .click();
    await panel
      .getByRole("group", { name: "确认生成商户RSA密钥对", exact: true })
      .getByRole("button", { name: "确定生成", exact: true })
      .click();
    await expect(
      panel.getByLabel("商户RSA私钥（仅本次显示）", { exact: true }),
    ).toHaveValue(privateKey);
    await expect(
      panel.getByLabel("商户RSA私钥（仅本次显示）", { exact: true }),
    ).toBeFocused();
    const stored = await page.evaluate(() =>
      [...Object.values(localStorage), ...Object.values(sessionStorage)].join(
        "\n",
      ),
    );
    expect(stored).not.toContain(privateKey);
    expect(stored).not.toContain(secret);
    await panel
      .getByRole("button", { name: "关闭密钥显示", exact: true })
      .click();
    await expect(
      panel.getByLabel("商户RSA私钥（仅本次显示）", { exact: true }),
    ).toHaveCount(0);
    await dialog.getByRole("tab", { name: "基本信息", exact: true }).click();
    await expect(
      dialog.getByRole("textbox", { name: "名称", exact: false }),
    ).toHaveValue("保留普通资料草稿");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
    expect(writes).toHaveLength(3);
    const saved = writes.find((w) => w.path.endsWith(`/merchants/${id}`))!.body;
    expect(saved).not.toHaveProperty("api_key");
    expect(saved).not.toHaveProperty("rsa_public_key");
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    await dialog.getByRole("tab", { name: "密钥信息", exact: true }).click();
    await expect(
      dialog.getByLabel("商户RSA私钥（仅本次显示）", { exact: true }),
    ).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
