import { test, expect } from "@playwright/test";
import { generateKeyPairSync } from "node:crypto";
const keys = generateKeyPairSync("rsa", {
  modulusLength: 2048,
  publicKeyEncoding: { type: "spki", format: "pem" },
  privateKeyEncoding: { type: "pkcs8", format: "pem" },
});
const fp = "a".repeat(64),
  next = "b".repeat(64);
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, message: "", data }),
});
for (const algorithm of ["MD5", "RSA"]) {
  test(`${algorithm} merchant explicitly generates RSA once and clears private on close/navigation`, async ({
    page,
  }) => {
    let calls = 0;
    let current = fp;
    await page.route("**/console/v1/**", (route) => {
      const p = new URL(route.request().url()).pathname;
      if (p.endsWith("/session"))
        return route.fulfill(
          ok({
            user: { id: "1000", name: "Merchant", role: "merchant" },
            csrf_token: "csrf-test",
          }),
        );
      if (p.endsWith("/rsa-key/rotate")) {
        calls++;
        expect(route.request().postDataJSON()).toEqual({
          current_password: "synthetic-password-123",
          expected_fingerprint: current,
        });
        current = next;
        return route.fulfill(
          ok({
            algorithm,
            public_key: keys.publicKey,
            private_key: keys.privateKey,
            fingerprint: next,
          }),
        );
      }
      return route.fulfill(
        ok({
          pid: "1000",
          algorithm,
          api_key_masked: "********last",
          merchant_public_key: current === next ? keys.publicKey : "",
          platform_public_key: "",
          rsa_fingerprint: current,
          can_rotate_rsa: true,
        }),
      );
    });
    await page.goto("/merchant/account/api-info");
    await page
      .getByRole("button", { name: "生成 RSA 密钥对", exact: true })
      .click();
    expect(calls).toBe(0);
    if (algorithm === "MD5")
      await expect(page.getByRole("dialog")).toContainText("签名算法保持 MD5");
    else
      await expect(page.getByRole("dialog")).toContainText(
        "旧 RSA 签名将立即失效",
      );
    await page.getByLabel("确认当前登录密码").fill("synthetic-password-123");
    await page
      .getByRole("button", { name: "确认生成 RSA 密钥对", exact: true })
      .click();
    await expect(page.getByLabel("新 RSA 私钥", { exact: true })).toHaveValue(
      keys.privateKey,
    );
    await page.setViewportSize({ width: 375, height: 812 });
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(375);
    expect(calls).toBe(1);
    expect(
      await page.evaluate(() => localStorage.length + sessionStorage.length),
    ).toBe(0);
    await page
      .getByRole("button", { name: "已保存，关闭私钥", exact: true })
      .click();
    await expect(page.getByLabel("新 RSA 私钥", { exact: true })).toHaveCount(
      0,
    );
    await page
      .getByRole("button", { name: "重置 RSA 密钥对", exact: true })
      .click();
    await page.getByLabel("确认当前登录密码").fill("secret-transient");
    await page.goto("/merchant/account/settings");
    await expect(page.getByLabel("新 RSA 私钥", { exact: true })).toHaveCount(
      0,
    );
    expect(calls).toBe(1);
  });
}
test("unknown RSA response is not retried and capability false hides action", async ({
  page,
}) => {
  let calls = 0,
    cap = true;
  await page.route("**/console/v1/**", (route) => {
    const p = new URL(route.request().url()).pathname;
    if (p.endsWith("/session"))
      return route.fulfill(
        ok({
          user: { id: "1000", name: "Merchant", role: "merchant" },
          csrf_token: "csrf-test",
        }),
      );
    if (p.endsWith("/rsa-key/rotate")) {
      calls++;
      cap = false;
      return route.abort("connectionfailed");
    }
    return route.fulfill(
      ok({
        pid: "1000",
        algorithm: "RSA",
        api_key_masked: "********last",
        merchant_public_key: "",
        platform_public_key: "",
        rsa_fingerprint: fp,
        can_rotate_rsa: cap,
      }),
    );
  });
  await page.goto("/merchant/account/api-info");
  await page
    .getByRole("button", { name: "生成 RSA 密钥对", exact: true })
    .click();
  await page.getByLabel("确认当前登录密码").fill("synthetic-password-123");
  await page
    .getByRole("button", { name: "确认生成 RSA 密钥对", exact: true })
    .click();
  await expect(
    page.getByText(
      "请核对当前商户公钥。RSA 私钥仅在成功响应展示，关闭后无法再次查询。",
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "生成 RSA 密钥对", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByLabel("新 RSA 私钥", { exact: true })).toHaveCount(0);
  expect(calls).toBe(1);
});
