import { test, expect, type Page } from "@playwright/test";
const id = "9007199254740993";
async function fixture(
  page: Page,
  options: {
    keytype?: 0 | 1;
    rsa?: boolean;
    delegated?: boolean;
    failure?: "conflict" | "malformed";
    deferWrite?: boolean;
  } = {},
) {
  const writes: unknown[] = [];
  let fp = "a".repeat(64),
    mode: number | null = options.keytype ?? null,
    release = () => {};
  const wait = new Promise<void>((r) => (release = r));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = {},
      status = 200;
    if (p.endsWith("/session"))
      data = {
        user: { id, role: "merchant", name: "本地合成商户" },
        csrf_token: "local-signature163",
        ...(options.delegated
          ? { delegated_by: { id: "1", name: "localadmin" } }
          : {}),
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/signature-policy")) {
      if (options.delegated) {
        status = 403;
        data = null;
      } else {
        if (req.method() === "PATCH") {
          writes.push(req.postDataJSON());
          if (options.deferWrite) await wait;
          if (options.failure === "conflict") {
            status = 409;
            data = null;
          } else {
            const body = req.postDataJSON();
            expect(body).toEqual({ keytype: 1, expected_fingerprint: fp });
            mode = body.keytype;
            fp = "b".repeat(64);
          }
          if (options.failure === "malformed") {
            await route.fulfill({
              status: 200,
              contentType: "application/json",
              body: '{"code":0,"data":',
            });
            return;
          }
        }
        if (status === 200)
          data = {
            merchant_id: id,
            keytype: mode,
            algorithm: "MD5",
            rsa_configured: options.rsa !== false,
            fingerprint: fp,
            can_update: true,
          };
      }
    } else if (p.endsWith("/event-subscription")) {
      data = {
        event_url: "",
        event_types: [],
        fingerprint: "c".repeat(64),
        consecutive_failures: 0,
        last_delivery_state: "none",
        subscription_notice: "",
      };
    } else if (p.endsWith("/api-info"))
      data = {
        pid: id,
        algorithm: "MD5",
        api_key_masked: "********test",
        merchant_public_key: "",
        platform_public_key: "",
        rsa_fingerprint: fp,
        can_rotate_rsa: true,
      };
    await route.fulfill({
      status,
      json: {
        code: status === 200 ? 0 : -1,
        message: status === 409 ? "changed" : "",
        data,
      },
    });
  });
  await page.goto("/merchant/account/api-info");
  return {
    writes,
    release,
    panel: page.getByRole("region", { name: "签名方式设置", exact: true }),
  };
}
test("API access organizes compatible signing, credentials and local docs on desktop and mobile", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const f = await fixture(page, { keytype: 0, rsa: false });
  const connection = page.getByRole("region", {
    name: "接入参数",
    exact: true,
  });
  const md5 = page.getByRole("region", { name: "MD5 API 密钥", exact: true });
  const rsa = page.getByRole("region", { name: "RSA 公钥", exact: true });
  await expect(
    page.getByRole("heading", { name: "API 接入", exact: true }),
  ).toBeVisible();
  await expect(
    connection.getByRole("link", { name: "开发文档" }),
  ).toHaveAttribute("href", "/doc/");
  await expect(
    connection.getByRole("button", { name: "复制商户ID" }),
  ).toBeVisible();
  await expect(f.panel.getByLabel("签名方式", { exact: true })).toHaveValue(
    "0",
  );
  await expect(
    f.panel.getByRole("button", { name: "保存签名方式" }),
  ).toBeDisabled();
  await expect(md5).toContainText("********test");
  await expect(rsa.getByText("未配置", { exact: true })).toHaveCount(2);
  await expect(rsa.getByRole("button", { name: /^复制/ })).toHaveCount(0);
  await expect(page.getByLabel("订阅URL")).toBeVisible();
  const desktop = await connection.boundingBox();
  const desktopPolicy = await f.panel.boundingBox();
  expect(desktop!.x + desktop!.width).toBeLessThan(desktopPolicy!.x);
  await page.screenshot({
    path: test.info().outputPath("api-access-desktop.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.setViewportSize({ width: 375, height: 812 });
  const mobile = await connection.boundingBox();
  const mobilePolicy = await f.panel.boundingBox();
  expect(mobile!.y + mobile!.height).toBeLessThan(mobilePolicy!.y);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(375);
  expect(f.writes).toHaveLength(0);
  await page.screenshot({
    path: test.info().outputPath("api-access-mobile.png"),
    fullPage: true,
    animations: "disabled",
  });
});
for (const width of [1280, 375])
  test(
    "original signature modes require explicit confirmation and preserve full owner at " +
      width,
    async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      const f = await fixture(page),
        select = f.panel.getByLabel("签名方式", { exact: true });
      await expect(f.panel).toContainText("仅MD5（历史配置）");
      await expect(
        select.getByRole("option", { name: "MD5+RSA兼容", exact: true }),
      ).toHaveAttribute("value", "0");
      await select.selectOption("1");
      await f.panel
        .getByRole("button", { name: "保存签名方式", exact: true })
        .click();
      const confirm = f.panel.getByRole("group", {
        name: "确认修改签名方式",
        exact: true,
      });
      await expect(confirm).toContainText(id);
      await expect(confirm).toContainText("仅RSA");
      expect(f.writes).toHaveLength(0);
      const cancel = confirm.getByRole("button", { name: "取消", exact: true });
      await expect(cancel).toBeFocused();
      await cancel.click();
      await expect(select).toBeFocused();
      expect(f.writes).toHaveLength(0);
      await select.selectOption("1");
      await f.panel
        .getByRole("button", { name: "保存签名方式", exact: true })
        .click();
      await confirm
        .getByRole("button", { name: "确定修改", exact: true })
        .click();
      await expect(f.panel.getByRole("status")).toContainText("签名方式已保存");
      await expect(select).toHaveValue("1");
      expect(f.writes).toHaveLength(1);
      expect(
        await page.evaluate(() => localStorage.length + sessionStorage.length),
      ).toBe(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await f.panel.screenshot({
        path: test.info().outputPath("signature-policy-" + width + ".png"),
      });
    },
  );
test("missing RSA key disables RSA-only mode and explains generation prerequisite", async ({
  page,
}) => {
  const f = await fixture(page, { rsa: false });
  await expect(
    f.panel
      .getByLabel("签名方式", { exact: true })
      .getByRole("option", { name: "仅RSA", exact: true }),
  ).toBeDisabled();
  await expect(f.panel).toContainText("请先生成商户 RSA 密钥对");
  expect(f.writes).toHaveLength(0);
});
test("delegated merchant session cannot select or save signature policy", async ({
  page,
}) => {
  const f = await fixture(page, { delegated: true });
  await expect(f.panel.getByRole("alert")).toBeVisible();
  await expect(f.panel.getByLabel("签名方式", { exact: true })).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "重置 API 密钥", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "生成 RSA 密钥对", exact: true }),
  ).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});
for (const failure of ["conflict", "malformed"] as const)
  test(
    "signature " + failure + " requires refresh rather than automatic retry",
    async ({ page }) => {
      const f = await fixture(page, { failure });
      await f.panel.getByLabel("签名方式", { exact: true }).selectOption("1");
      await f.panel
        .getByRole("button", { name: "保存签名方式", exact: true })
        .click();
      await f.panel
        .getByRole("button", { name: "确定修改", exact: true })
        .click();
      await expect(f.panel.getByRole("alert")).toBeVisible();
      await expect(f.panel.getByRole("alert")).toBeFocused();
      expect(f.writes).toHaveLength(1);
      await expect(f.panel.getByLabel("签名方式", { exact: true })).toHaveCount(
        0,
      );
    },
  );
test("pending signature write blocks repeat submit, cancel and background focus", async ({
  page,
}) => {
  const f = await fixture(page, { deferWrite: true });
  await f.panel.getByLabel("签名方式", { exact: true }).selectOption("1");
  await f.panel
    .getByRole("button", { name: "保存签名方式", exact: true })
    .click();
  const confirm = f.panel.getByRole("group", {
    name: "确认修改签名方式",
    exact: true,
  });
  await confirm.getByRole("button", { name: "确定修改", exact: true }).click();
  await expect.poll(() => f.writes.length).toBe(1);
  await expect(
    confirm.getByRole("button", { name: "取消", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press("Tab");
  await expect(confirm).toBeFocused();
  await page.keyboard.press("Enter");
  expect(f.writes).toHaveLength(1);
  f.release();
  await expect(f.panel.getByRole("status")).toContainText("签名方式已保存");
});
