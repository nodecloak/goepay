import { pageReload248 } from "./fixtures/page-reload248";
import { expect, test, type Page } from "@playwright/test";
import { accountSettingsReadFixture } from "./fixtures/account-settings";

for (const width of [1280, 375]) {
  test(`recipient inline editing keeps keyboard focus and discards cancelled private drafts at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const state = await fixture(page);
    const opener = page.getByRole("button", {
      name: "配置收款资料",
      exact: true,
    });
    await opener.click();
    await expect(
      page.getByRole("button", { name: "收款方式", exact: true }),
    ).toBeFocused();
    await page
      .getByRole("textbox", { name: "收款账号", exact: true })
      .fill("synthetic-cancelled-account-224");
    await page
      .getByRole("textbox", { name: "真实姓名", exact: true })
      .fill("Synthetic Holder");
    const cancel = page.getByRole("button", {
      name: "取消修改收款资料",
      exact: true,
    });
    await cancel.click();
    await expect(opener).toBeFocused();
    await opener.click();
    await expect(
      page.getByRole("textbox", { name: "收款账号", exact: true }),
    ).toHaveValue("");
    await expect(
      page.getByRole("textbox", { name: "真实姓名", exact: true }),
    ).toHaveValue("");
    await cancel.click();
    await expect(opener).toBeFocused();
    expect(state.writes).toEqual([]);
    expect(state.errors).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  });
}

async function fixture(
  page: Page,
  failure?: "conflict" | "unknown" | "delegated",
) {
  const reads: string[] = [],
    writes: { path: string; body: Record<string, unknown> }[] = [],
    errors: string[] = [];
  let state = {
    merchant_id: "1000",
    qq: "123456",
    pay_minmoney: "1.00",
    pay_maxmoney: "2.00",
    remain_money: "10%",
    bounds_valid: true,
    fingerprint: "a".repeat(64),
    can_update: true,
  };
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    let data: unknown = { items: [], total: 0 };
    if (req.method() === "GET") reads.push(path);
    else writes.push({ path, body: req.postDataJSON() });
    if (path.endsWith("/session"))
      data = {
        user: { id: "1000", role: "merchant", name: "本地商户" },
        csrf_token: "synthetic-reserve-session",
        ...(failure === "delegated"
          ? { delegated_by: { id: "2", name: "本地管理员" } }
          : {}),
      };
    else if (path.endsWith("/account/personal-settings")) {
      if (req.method() === "PATCH") {
        if (failure === "conflict")
          return route.fulfill({
            status: 409,
            json: { code: -1, message: "changed" },
          });
        const body = req.postDataJSON();
        expect(body.expected_fingerprint).toBe(state.fingerprint);
        state = {
          ...state,
          qq: body.qq,
          pay_minmoney: body.pay_minmoney,
          pay_maxmoney: body.pay_maxmoney,
          remain_money: body.remain_money,
          fingerprint: String.fromCharCode(97 + writes.length).repeat(64),
        };
        if (failure === "unknown") return route.abort();
      }
      data = state;
    } else if (path.endsWith("/settlement-recipient/allowed-types"))
      data = { settlement: ["alipay", "wxpay", "bank"], can_write: true };
    else if (path.endsWith("/settlement-recipient"))
      data = {
        ...(req.method() === "GET" ? { can_write: true } : {}),
        state: "missing",
        config_id: "0",
        type: "",
        app_id: "",
        account_masked: "",
        name_masked: "",
      };
    else if (path.endsWith("/account/profile"))
      data = {
        name: "本地商户",
        email: "",
        pay: "1",
        uid: "1000",
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
    else data = accountSettingsReadFixture(path) ?? data;
    await route.fulfill({ json: { code: 0, message: "", data } });
  });
  await page.goto("/merchant/settle/settings");
  const reserve = page.getByRole("region", {
    name: "预留余额设置",
    exact: true,
  });
  await expect(
    page.getByRole("heading", { name: "收款账号设置", exact: true }),
  ).toBeVisible();
  await expect(reserve).toBeVisible();
  return { reserve, reads, writes, errors };
}

for (const width of [1440, 375])
  test(`settlement settings place reserve beside recipient setup and keep section saves independent at ${width}px`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await expect(f.reserve.getByLabel("预留余额", { exact: true })).toHaveValue(
      "10%",
    );
    await expect(
      f.reserve.locator(
        '[name="qq"], [name="pay_minmoney"], [name="pay_maxmoney"]',
      ),
    ).toHaveCount(0);
    await f.reserve.getByLabel("预留余额", { exact: true }).fill("20%");
    await f.reserve
      .getByRole("button", { name: "取消修改", exact: true })
      .click();
    await expect(f.reserve.getByLabel("预留余额", { exact: true })).toHaveValue(
      "10%",
    );
    expect(f.writes).toHaveLength(0);
    await f.reserve.getByLabel("预留余额", { exact: true }).fill("25%");
    await f.reserve
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect(f.reserve.getByRole("status")).toHaveText("保存成功");
    expect(f.writes).toEqual([
      {
        path: "/console/v1/merchant/account/personal-settings",
        body: {
          qq: "123456",
          pay_minmoney: "1.00",
          pay_maxmoney: "2.00",
          remain_money: "25%",
          expected_fingerprint: "a".repeat(64),
        },
      },
    ]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await f.reserve.screenshot({
      path: info.outputPath("settlement-reserve-" + width + ".png"),
    });
    await page.goto("/merchant/account/settings");
    const contact = page.getByRole("region", {
      name: "个人功能设置",
      exact: true,
    });
    await expect(contact.getByLabel("联系QQ")).toHaveValue("123456");
    await expect(page.locator('[name="remain_money"]')).toHaveCount(0);
    await contact.getByLabel("联系QQ").fill("7654321");
    await contact
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect(contact.getByRole("status")).toHaveText("保存成功");
    expect(f.writes).toHaveLength(2);
    expect(f.writes[1].body).toEqual({
      qq: "7654321",
      pay_minmoney: "1.00",
      pay_maxmoney: "2.00",
      remain_money: "25%",
      expected_fingerprint: "b".repeat(64),
    });
    expect(f.errors).toHaveLength(0);
  });

for (const failure of ["conflict", "unknown"] as const)
  test(`reserve ${failure} requires explicit read without replaying a save or touching recipient data`, async ({
    page,
  }) => {
    const f = await fixture(page, failure);
    await expect(f.reserve.getByLabel("预留余额", { exact: true })).toHaveValue(
      "10%",
    );
    await f.reserve.getByLabel("预留余额", { exact: true }).fill("25%");
    await f.reserve
      .getByRole("button", { name: "保存设置", exact: true })
      .click();
    await expect(f.reserve.getByRole("alert")).toContainText("刷新页面");
    await expect(
      f.reserve.getByRole("button", { name: "保存设置", exact: true }),
    ).toBeDisabled();
    expect(f.writes).toHaveLength(1);
    expect(
      f.reads.filter((p) => p.endsWith("/account/personal-settings")),
    ).toHaveLength(1);
    await pageReload248(page);
    await expect(f.reserve.getByLabel("预留余额", { exact: true })).toHaveValue(
      failure === "unknown" ? "25%" : "10%",
    );
    expect(f.writes).toHaveLength(1);
    expect(f.errors).toHaveLength(0);
  });

test("delegated settlement settings never load the merchant personal reserve", async ({
  page,
}) => {
  const f = await fixture(page, "delegated");
  await expect(f.reserve).toContainText(
    "个人设置需要商户本人登录后查看和修改。",
  );
  await expect(f.reserve.locator("input")).toHaveCount(0);
  expect(
    f.reads.filter((p) => p.endsWith("/account/personal-settings")),
  ).toHaveLength(0);
  expect(f.writes).toHaveLength(0);
  expect(f.errors).toHaveLength(0);
});
