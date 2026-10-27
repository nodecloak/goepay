import { test, expect, type Page } from "@playwright/test";
const id = "9007199254740993";
async function fixture(
  page: Page,
  options: { operator?: boolean; uncertain?: boolean } = {},
) {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  let fp = "a".repeat(64),
    release = () => {};
  const wait = new Promise<void>((r) => (release = r));
  const record = {
    id,
    name: "合成商户",
    signature_algorithm: "MD5",
    keytype: options.operator ? 1 : (null as number | null),
  };
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = { items: [], total: 0 };
    if (req.method() !== "GET")
      writes.push({ path: p, body: req.postDataJSON() });
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: options.operator ? "operator" : "super",
        },
        csrf_token: "admin-policy163",
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
          {
            name: "keytype",
            label: "签名方式",
            type: "string",
            read_only: true,
          },
        ],
      };
    else if (p.endsWith("/signature-policy")) {
      if (req.method() === "PATCH") {
        expect(req.postDataJSON()).toEqual({
          keytype: 1,
          expected_fingerprint: fp,
        });
        await wait;
        record.keytype = 1;
        fp = "b".repeat(64);
        if (options.uncertain) {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: '{"code":0,"data":',
          });
          return;
        }
      }
      data = {
        merchant_id: id,
        keytype: record.keytype,
        algorithm: "MD5",
        rsa_configured: true,
        fingerprint: fp,
        can_update: true,
      };
    } else if (p.endsWith("/credentials"))
      data = {
        merchant_id: id,
        revision: "1",
        algorithm: "MD5",
        public_key: "",
        md5_configured: true,
        fingerprint: "c".repeat(64),
      };
    else if (p.endsWith("/resources/merchants/" + id)) {
      if (
        req.method() === "PATCH" &&
        typeof req.postDataJSON().name === "string"
      )
        record.name = req.postDataJSON().name;
      data = record;
    } else if (p.endsWith("/resources/merchants"))
      data = { items: [record], total: 1 };
    await route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/admin/merchant/merchant-list");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "编辑商户管理",
    exact: true,
  });
  await dialog
    .getByRole("textbox", { name: "名称", exact: false })
    .fill("保留普通资料草稿163");
  await dialog.getByRole("tab", { name: "密钥信息", exact: true }).click();
  return {
    writes,
    release,
    dialog,
    panel: dialog.getByRole("region", { name: "签名方式设置", exact: true }),
  };
}
for (const width of [1280, 375])
  test(
    "admin edits actual signature policy separately from legacy algorithm at " +
      width,
    async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      const f = await fixture(page),
        select = f.panel.getByLabel("签名方式", { exact: true });
      await expect(select).toBeVisible();
      await f.dialog
        .getByRole("combobox", { name: "签名算法", exact: true })
        .selectOption("RSA");
      await select.selectOption("1");
      await f.panel
        .getByRole("button", { name: "保存签名方式", exact: true })
        .click();
      const confirmation = f.panel.getByRole("group", {
        name: "确认修改签名方式",
        exact: true,
      });
      await expect(confirmation).toContainText(id);
      await confirmation
        .getByRole("button", { name: "取消", exact: true })
        .click();
      await expect(
        f.dialog.getByRole("combobox", { name: "签名算法", exact: true }),
      ).toHaveValue("RSA");
      expect(f.writes).toHaveLength(0);
      await select.selectOption("1");
      await f.panel
        .getByRole("button", { name: "保存签名方式", exact: true })
        .click();
      await confirmation
        .getByRole("button", { name: "确定修改", exact: true })
        .click();
      await expect.poll(() => f.writes.length).toBe(1);
      await expect(
        f.dialog.getByRole("button", { name: "正在处理…", exact: true }),
      ).toBeDisabled();
      await expect(
        f.dialog.getByRole("button", { name: "重置MD5密钥", exact: true }),
      ).toBeDisabled();
      await f.dialog.press("Escape");
      await expect(f.dialog).toBeVisible();
      f.release();
      await expect(f.panel.getByRole("status")).toContainText("签名方式已保存");
      await expect(
        f.dialog.getByRole("combobox", { name: "签名算法", exact: true }),
      ).toHaveCount(0);
      await expect(select).toHaveValue("1");
      await expect(select).toBeFocused();
      await f.panel.screenshot({
        path: test.info().outputPath("admin-policy-" + width + ".png"),
      });
      await f.dialog
        .getByRole("tab", { name: "基本信息", exact: true })
        .click();
      await expect(
        f.dialog.getByRole("textbox", { name: "名称", exact: false }),
      ).toHaveValue("保留普通资料草稿163");
      await f.dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
      expect(f.writes).toHaveLength(2);
      expect(f.writes[1]!.body).toMatchObject({
        name: "保留普通资料草稿163",
        signature_algorithm: "MD5",
      });
      expect(f.writes[1]!.body).not.toHaveProperty("keytype");
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    },
  );
test("uncertain admin policy response cannot submit the stale legacy algorithm draft later", async ({
  page,
}) => {
  const f = await fixture(page, { uncertain: true });
  await expect(f.panel.getByLabel("签名方式", { exact: true })).toBeVisible();
  await f.dialog
    .getByRole("combobox", { name: "签名算法", exact: true })
    .selectOption("RSA");
  await f.panel.getByLabel("签名方式", { exact: true }).selectOption("1");
  await f.panel
    .getByRole("button", { name: "保存签名方式", exact: true })
    .click();
  await f.panel.getByRole("button", { name: "确定修改", exact: true }).click();
  f.release();
  await expect(f.panel.getByRole("alert")).toContainText(
    "本次签名方式修改未能确认",
  );
  await f.dialog.getByRole("tab", { name: "基本信息", exact: true }).click();
  await f.dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("修改已保存", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(2);
  expect(f.writes[1]!.body).toHaveProperty("signature_algorithm", "MD5");
  expect(f.writes[1]!.body).not.toHaveProperty("keytype");
});
test("operator sees the actual explicit policy without a misleading legacy selector or super controls", async ({
  page,
}) => {
  const f = await fixture(page, { operator: true });
  await expect(
    f.dialog.getByText("当前签名方式：仅RSA。历史算法不决定当前方式。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    f.dialog.getByRole("combobox", { name: "签名算法", exact: true }),
  ).toHaveCount(0);
  await expect(f.panel).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});
