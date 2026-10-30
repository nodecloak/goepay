import { test, expect, type Page } from "@playwright/test";
import { accountSettingsReadFixture } from "./fixtures/account-settings";
const mid = "9007199254740993";
async function fixture(page: Page, conflict = false) {
  const writes: { path: string; body: Record<string, unknown> }[] = [],
    reads: string[] = [],
    errors: string[] = [];
  let state = {
    merchant_id: mid,
    qq: "123456",
    pay_minmoney: "1.00",
    pay_maxmoney: "2.00",
    remain_money: "",
    bounds_valid: true,
    fingerprint: "a".repeat(64),
    can_update: true,
  };
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    if (request.method() === "GET") reads.push(path);
    else writes.push({ path, body: request.postDataJSON() });
    let data: unknown = { items: [], total: 0 };
    if (path.endsWith("/session"))
      data = {
        user: { id: mid, name: "本地商户", role: "merchant" },
        csrf_token: "c".repeat(43),
      };
    else if (path.endsWith("/account/personal-settings")) {
      if (request.method() === "PATCH") {
        if (conflict)
          return route.fulfill({
            status: 409,
            contentType: "application/json",
            body: JSON.stringify({ code: -1, message: "changed" }),
          });
        const body = request.postDataJSON();
        expect(body).toEqual({
          qq: "7654321",
          pay_minmoney: "1.50",
          pay_maxmoney: "2.00",
          remain_money: "",
          expected_fingerprint: state.fingerprint,
        });
        state = {
          ...state,
          qq: body.qq,
          pay_minmoney: body.pay_minmoney,
          pay_maxmoney: body.pay_maxmoney,
          remain_money: body.remain_money,
          fingerprint: "b".repeat(64),
        };
      }
      data = state;
    } else if (path.endsWith("/account/profile"))
      data = { name: "本地商户", email: "local172@example.invalid", pay: "1" };
    else if (path.endsWith("/security/totp")) data = { enabled: false };
    else data = accountSettingsReadFixture(path, mid) ?? data;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/merchant/account/settings");
  const panel = page.getByRole("region", { name: "个人功能设置", exact: true });
  await expect(panel.getByLabel("联系QQ")).toHaveValue("123456");
  return { panel, reads, writes, errors };
}
for (const width of [1440, 375])
  test(`personal172 original fields, cancel and exact save at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await f.panel.getByLabel("联系QQ").fill("999999");
    await f.panel
      .getByRole("button", { name: "取消修改", exact: true })
      .click();
    await expect(f.panel.getByLabel("联系QQ")).toHaveValue("123456");
    expect(f.writes).toHaveLength(0);
    await f.panel.getByLabel("联系QQ").fill("7654321");
    await f.panel.getByLabel(/最小支付金额限制/).fill("1.5");
    await f.panel
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect(f.panel.getByRole("status")).toHaveText("保存成功");
    expect(f.writes).toHaveLength(1);
    await expect(f.panel.getByLabel(/最小支付金额限制/)).toHaveValue("1.50");
    await expect(
      f.panel.getByText(/平台、用户组和通道限制仍然生效/),
    ).toBeVisible();
    const box = await f.panel.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await f.panel.evaluate((element) =>
      element.scrollIntoView({ block: "center" }),
    );
    await expect
      .poll(async () => (await f.panel.boundingBox())!.y)
      .toBeGreaterThanOrEqual(60);
    await f.panel.screenshot({
      path: info.outputPath(`personal-settings-${width}.png`),
      animations: "disabled",
    });
    expect(f.errors).toHaveLength(0);
    expect(
      await page.evaluate(
        () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
      ),
    ).not.toContain("7654321");
  });
test("personal172 conflicting settings require explicit refresh before another save", async ({
  page,
}) => {
  const f = await fixture(page, true);
  const before = f.reads.filter((p) =>
    p.endsWith("/account/personal-settings"),
  ).length;
  await f.panel.getByRole("button", { name: "保存设置", exact: true }).click();
  await expect(f.panel.getByRole("alert")).toContainText("重新读取");
  await expect(
    f.panel.getByRole("button", { name: "保存设置", exact: true }),
  ).toBeDisabled();
  expect(f.writes).toHaveLength(1);
  expect(
    f.reads.filter((p) => p.endsWith("/account/personal-settings")),
  ).toHaveLength(before);
  await f.panel.getByRole("button", { name: "重新读取", exact: true }).click();
  await expect(f.panel.getByLabel("联系QQ")).toHaveValue("123456");
  expect(f.writes).toHaveLength(1);
  expect(f.errors).toHaveLength(0);
});
