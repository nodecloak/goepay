import { expect, test, type Page } from "@playwright/test";
import { accountSettingsReadFixture } from "./fixtures/account-settings";

const recipient = (id = "42") => ({
  state: "configured",
  config_id: id,
  type: "alipay",
  app_id: "",
  account_masked: "***1234",
  name_masked: "***",
});
async function fixture(page: Page, empty = false) {
  let version = "42",
    posts = 0,
    reads = 0,
    readbackFails = true;
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/console/v1/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname;
    let data: unknown = accountSettingsReadFixture(path, "1000") ?? {
      items: [],
      total: 0,
    };
    if (path.endsWith("/session"))
      data = {
        user: { id: "1000", role: "merchant", name: "Synthetic Merchant" },
        csrf_token: "synthetic-panel-session",
      };
    else if (path.endsWith("/settlement-recipient/allowed-types")) {
      expect(url.search).toBe("");
      data = { settlement: empty ? [] : ["alipay"], can_write: true };
    } else if (path.endsWith("/settlement-recipient")) {
      if (request.method() === "POST") {
        posts++;
        expect(request.postDataJSON()).toMatchObject({
          expected_config_id: "42",
          account: "synthetic-private-recipient",
          name: "Synthetic Private Payee",
        });
        version = "43";
        return route.abort();
      }
      reads++;
      if (posts && readbackFails)
        return route.fulfill({
          status: 503,
          json: { code: -1, message: "Synthetic readback unavailable" },
        });
      data = { ...recipient(version), can_write: true };
    } else if (path.endsWith("/account/profile"))
      data = {
        uid: "1000",
        name: "Synthetic Merchant",
        email: "",
        pay: "1",
        has_pwd: true,
        registration_time_known: false,
      };
    else if (path.endsWith("/account/buyer-fees"))
      data = {
        merchant_id: "1000",
        mode: 0,
        mode_feerate: "",
        configured: false,
        revision: "0",
        fingerprint: "a".repeat(64),
        can_update: true,
        partial_available: true,
      };
    else if (path.endsWith("/public/merchant-access"))
      data = { close_keylogin: 0, user_refund: 1 };
    else if (path.endsWith("/security/totp")) data = { enabled: false };
    return route.fulfill({ status: 200, json: { code: 0, data } });
  });
  await page.goto("/merchant/settle/settings");
  const panel = page.locator("fieldset.settlement-recipient");
  await expect(panel.getByTestId("settlement-recipient-current")).toContainText(
    "版本 42",
  );
  return {
    panel,
    pageErrors,
    posts: () => posts,
    reads: () => reads,
    allowReadback: () => {
      readbackFails = false;
    },
  };
}

test("empty allowed settlement list keeps original masked recipient readable and blocks new configuration", async ({
  page,
}) => {
  const f = await fixture(page, true);
  await expect(f.panel).toContainText("当前没有可用的结算收款方式");
  await expect(
    f.panel.getByRole("button", { name: "修改收款资料", exact: true }),
  ).toBeDisabled();
  await expect(
    f.panel.getByRole("button", { name: "刷新收款版本", exact: true }),
  ).toBeEnabled();
  expect(f.posts()).toBe(0);
  expect(f.pageErrors).toEqual([]);
});

test("unknown recipient save clears private fields and needs successful explicit readback before another edit", async ({
  page,
}) => {
  const f = await fixture(page);
  await f.panel
    .getByRole("button", { name: "修改收款资料", exact: true })
    .click();
  await f.panel
    .getByLabel("收款账号", { exact: true })
    .fill("synthetic-private-recipient");
  await f.panel
    .getByLabel("真实姓名", { exact: true })
    .fill("Synthetic Private Payee");
  await f.panel
    .getByRole("button", { name: "保存收款版本", exact: true })
    .click();
  await expect(f.panel).toContainText("保存结果尚未确认");
  await expect(f.panel.getByLabel("收款账号", { exact: true })).toHaveCount(0);
  await expect(
    f.panel.getByRole("button", { name: "修改收款资料", exact: true }),
  ).toBeDisabled();
  expect(f.posts()).toBe(1);
  const before = f.reads();
  await f.panel
    .getByRole("button", { name: "刷新收款版本", exact: true })
    .click();
  await expect(f.panel).toContainText("Synthetic readback unavailable");
  await expect(f.panel).toContainText("保存结果尚未确认");
  expect(f.reads()).toBe(before + 1);
  expect(f.posts()).toBe(1);
  f.allowReadback();
  await f.panel
    .getByRole("button", { name: "刷新收款版本", exact: true })
    .click();
  await expect(
    f.panel.getByTestId("settlement-recipient-current"),
  ).toContainText("版本 43");
  await expect(
    f.panel.getByRole("button", { name: "修改收款资料", exact: true }),
  ).toBeEnabled();
  expect(f.posts()).toBe(1);
  expect(f.pageErrors).toEqual([]);
  expect(
    await page.evaluate(() => JSON.stringify([localStorage, sessionStorage])),
  ).not.toContain("synthetic-private-recipient");
});
