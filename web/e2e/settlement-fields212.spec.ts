import { test, expect } from "@playwright/test";

test("merchant recipient form focuses specific invalid fields and offers configured bank codes", async ({
  page,
}, testInfo) => {
  let writes = 0;
  await page.setViewportSize({ width: 375, height: 812 });
  await page.route("**/console/v1/**", (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    if (req.method() !== "GET") writes++;
    const data = p.endsWith("/session")
      ? { user: { id: "1000", role: "merchant" }, csrf_token: "fields212" }
      : p.endsWith("/allowed-types")
        ? { settlement: ["alipay", "wxpay", "bank"], can_write: true }
        : p.endsWith("/settlement-recipient")
          ? {
              ...(req.method() === "GET" ? { can_write: true } : {}),
              state: "missing",
              config_id: "0",
              type: "",
              app_id: "",
              account_masked: "",
              name_masked: "",
            }
          : { items: [], total: 0 };
    return route.fulfill({ json: { code: 0, data } });
  });
  await page.goto("/merchant/settle/settings");
  await page.getByRole("button", { name: "配置收款资料", exact: true }).click();
  await page.getByRole("button", { name: "保存收款版本", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "收款账号", exact: true }),
  ).toBeFocused();
  await expect(
    page.getByRole("textbox", { name: "真实姓名", exact: true }),
  ).toHaveAttribute("aria-invalid", "true");
  await page.getByRole("button", { name: "收款方式", exact: true }).click();
  await page.getByRole("option", { name: "微信", exact: true }).click();
  await page
    .getByRole("textbox", { name: "收款账号", exact: true })
    .fill("synthetic-openid");
  await page
    .getByRole("textbox", { name: "真实姓名", exact: true })
    .fill("Synthetic Name");
  await page.getByRole("button", { name: "保存收款版本", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "收款所属 AppID", exact: true }),
  ).toBeFocused();
  await expect(page.locator("main")).toContainText(
    "AppID与OpenID必须属于同一应用",
  );
  await page.getByRole("button", { name: "收款方式", exact: true }).click();
  await page.getByRole("option", { name: "银行卡", exact: true }).click();
  await page.getByRole("button", { name: "选择开户银行", exact: true }).click();
  await page
    .getByRole("option", { name: "工商银行（ICBC · 1002）", exact: true })
    .click();
  await page
    .getByRole("textbox", { name: "收款账号", exact: true })
    .fill("not-a-card");
  await page.getByRole("button", { name: "保存收款版本", exact: true }).click();
  await expect(
    page.getByRole("textbox", { name: "收款账号", exact: true }),
  ).toBeFocused();
  await expect(
    page.getByRole("alert").filter({ hasText: "银行卡号应为8至32位数字" }),
  ).toBeVisible();
  expect(writes).toBe(0);
  await page.screenshot({
    path: testInfo.outputPath("recipient-field-errors.png"),
    fullPage: true,
  });
});
