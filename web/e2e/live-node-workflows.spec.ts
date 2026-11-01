import {
  test,
  expect,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import { createHash, randomUUID } from "node:crypto";
import { readLocalSandboxEnv, localSandboxBase } from "./local-sandbox-env";

test.describe("isolated real backend node workflows", () => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1" ||
      process.env.GOEPAY_LIVE_NODE_WORKFLOWS_E2E !== "1",
    "Explicit separate local simulation workflow suite only",
  );
  let api: APIRequestContext,
    csrf = "",
    cookies: Awaited<ReturnType<APIRequestContext["storageState"]>>["cookies"];
  let env: Record<string, string>;
  const tag = "browser-" + Date.now();
  async function call(path: string, method = "GET", data?: unknown) {
    const r = await api.fetch("/console/v1/admin" + path, {
      method,
      data,
      headers: method === "GET" ? {} : { "X-CSRF-Token": csrf },
    });
    const body = await r.json();
    expect(r.ok(), body.message).toBeTruthy();
    expect(body.code, body.message).toBe(0);
    return body.data;
  }
  test.beforeAll(async ({ playwright }) => {
    localSandboxBase();
    env = Object.fromEntries(
      readLocalSandboxEnv(import.meta.url)
        .trim()
        .split(/\r?\n/)
        .map((line) => {
          const i = line.indexOf("=");
          return [line.slice(0, i), line.slice(i + 1)];
        }),
    );
    expect(env.GOEPAY_SANDBOX).toBe("true");
    expect(env.GOEPAY_PROVIDER_MODE).toBe("simulation");
    api = await playwright.request.newContext({ baseURL: localSandboxBase() });
    const login = await api.post("/console/v1/admin/login", {
      data: { username: "admin", password: env.GOEPAY_SEED_PASSWORD },
    });
    expect(login.ok()).toBeTruthy();
    const session = await login.json();
    expect(session.code).toBe(0);
    csrf = session.data.csrf_token;
    expect((await call("/dashboard")).sandbox).toBe(true);
    cookies = (await api.storageState()).cookies;
  });
  test.beforeEach(async ({ context }) => {
    await context.addCookies(cookies);
    // Browser actions cannot leave this disposable local environment.
    await context.route("**/*", (route) =>
      new URL(route.request().url()).origin === localSandboxBase()
        ? route.continue()
        : route.abort(),
    );
  });
  test.afterAll(async () => {
    await api?.dispose();
  });

  test("group create, rate edit, persisted reload, detail and delete", async ({
    page,
  }) => {
    await page.goto("/admin/merchant/group-list");
    await page.getByRole("button", { name: "新增用户组", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("显示名称", { exact: false }).fill(tag + "-group");
    await dialog.getByLabel("费率（万分比）", { exact: true }).fill("38");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    let row = page.getByRole("row").filter({
      has: page.getByRole("cell", { name: tag + "-group", exact: true }),
    });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(
      dialog.getByLabel("费率（万分比）", { exact: true }),
    ).toHaveValue("38");
    await dialog
      .getByLabel("显示名称", { exact: false })
      .fill(tag + "-updated");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await page.reload();
    row = page.getByRole("row").filter({
      has: page.getByRole("cell", { name: tag + "-updated", exact: true }),
    });
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(dialog.getByLabel("显示名称", { exact: false })).toHaveValue(
      tag + "-updated",
    );
    await expect(
      dialog.getByLabel("费率（万分比）", { exact: true }),
    ).toHaveValue("38");
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
    await expect(row).toHaveCount(0);
  });

  test("contact merchant creation opens its own persisted merchant session without payment intents", async ({
    page,
    browser,
  }) => {
    const email = `browser+${randomUUID()}@example.invalid`,
      password = `synthetic-${randomUUID()}`;
    await page.goto("/admin/merchant/merchant-list");
    await page.getByRole("button", { name: "新增商户", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.locator("#field-id")).toHaveCount(0);
    await expect(dialog.locator("#field-name")).toHaveCount(0);
    await dialog.getByLabel("邮箱(登录账号)", { exact: true }).fill(email);
    await dialog.getByLabel("登录密码", { exact: true }).fill(password);
    const response = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/console/v1/admin/resources/merchants" &&
        r.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: "确定添加", exact: true }).click();
    const created = await response,
      body = await created.json();
    expect(created.ok(), body.message).toBeTruthy();
    expect(body.code, body.message).toBe(0);
    expect(body.data.id).toMatch(/^[1-9]\d*$/);
    expect(JSON.stringify(body)).not.toContain(password);
    await expect(dialog).toHaveCount(0);

    const merchant = await browser.newContext({ baseURL: localSandboxBase() });
    let account: Page | undefined;
    try {
      await merchant.route("**/*", (route) =>
        new URL(route.request().url()).origin === localSandboxBase()
          ? route.continue()
          : route.abort(),
      );
      const login = await merchant.request.post("/console/v1/merchant/login", {
        data: { username: email, password },
      });
      expect(login.ok()).toBeTruthy();
      expect((await login.json()).code).toBe(0);
      const session = await (
        await merchant.request.get("/console/v1/merchant/session")
      ).json();
      expect(session.data.user).toMatchObject({
        id: body.data.id,
        role: "merchant",
      });
      const profile = await (
        await merchant.request.get("/console/v1/merchant/account/profile")
      ).json();
      expect(profile.code).toBe(0);
      expect(profile.data).toMatchObject({
        uid: body.data.id,
        name: email,
        email,
      });
      account = await merchant.newPage();
      await account.goto("/merchant/account/settings");
      await expect(
        account.getByRole("heading", {
          name: "联系方式与功能设置",
          exact: true,
        }),
      ).toBeVisible();
      await expect(
        account
          .getByRole("region", { name: "账户资料与安全设置", exact: true })
          .getByText(email, { exact: true }),
      ).toBeVisible();
      const orders = await (
        await merchant.request.get("/console/v1/merchant/orders?limit=20")
      ).json();
      expect(orders.code).toBe(0);
      expect(orders.data.total).toBe(0);
    } finally {
      if (account && !account.isClosed())
        await account.screenshot({
          path: test.info().outputPath("created-merchant-account.png"),
          fullPage: true,
        });
      await merchant.close();
    }
  });

  test("encrypted application drafts create, retain the original credential on edit and archive through the real API", async ({
    page,
  }) => {
    const name = tag + "-weixin",
      appID = "wx" + randomUUID().replaceAll("-", "").slice(0, 16),
      secret = randomUUID().replaceAll("-", "");
    const genericRequests: string[] = [];
    page.on("request", (request) => {
      if (new URL(request.url()).pathname.includes("/resources/weixin-apps"))
        genericRequests.push(request.url());
    });
    await page.goto("/admin/payapi/pay-weixin");
    await page.getByRole("button", { name: "新增应用", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "新增应用", exact: true });
    await dialog.getByLabel("名称", { exact: true }).fill(name);
    await dialog.getByLabel("APPID", { exact: true }).fill(appID);
    await dialog.getByLabel("APPSECRET", { exact: true }).fill(secret);
    await expect(
      dialog.getByLabel("启用资料", { exact: true }),
    ).not.toBeChecked();
    const createResponse = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/console/v1/admin/weixin-applications" &&
        r.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: "保存应用", exact: true }).click();
    const createResult = await createResponse,
      created = await createResult.json();
    expect(createResult.ok(), created.message).toBeTruthy();
    expect(created.code).toBe(0);
    expect(created.data).toMatchObject({
      name,
      app_id: appID,
      source_channel_id: "0",
      merchant_ids: [],
      enabled: false,
      draft: true,
      execution_ready: false,
      secret_configured: true,
      origin: "managed",
    });
    expect(JSON.stringify(created)).not.toContain(secret);
    await expect(dialog).toHaveCount(0);
    const row = page.locator(
      `tr[data-app-origin="managed"][data-app-id="${created.data.id}"]`,
    );
    await expect(row).toContainText("待关联草稿");
    await expect(row).toContainText("已配置（不回显）");
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const edit = page.getByRole("dialog", { name: "编辑应用", exact: true });
    await expect(edit.getByLabel("APPSECRET", { exact: true })).toHaveValue("");
    await edit.getByLabel("名称", { exact: true }).fill(name + "-updated");
    const patchResponse = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname ===
          `/console/v1/admin/weixin-applications/${created.data.id}` &&
        r.request().method() === "PATCH",
    );
    await edit.getByRole("button", { name: "保存应用", exact: true }).click();
    const patchResult = await patchResponse,
      patched = await patchResult.json();
    expect(patchResult.ok(), patched.message).toBeTruthy();
    expect(patched.code).toBe(0);
    expect(patched.data.revision).not.toBe(created.data.revision);
    expect(patched.data).toMatchObject({
      name: name + "-updated",
      secret_configured: true,
      source_channel_id: "0",
      merchant_ids: [],
      enabled: false,
      draft: true,
    });
    expect(JSON.stringify(patched)).not.toContain(secret);
    await expect(edit).toHaveCount(0);
    await expect(row).toContainText(name + "-updated");
    const listed = await call("/weixin-applications");
    expect(
      listed.items.find(
        (item: { id: string; origin: string }) =>
          item.id === created.data.id && item.origin === "managed",
      ),
    ).toMatchObject({
      revision: patched.data.revision,
      secret_configured: true,
    });
    expect(JSON.stringify(listed)).not.toContain(secret);
    expect(
      await page.evaluate(
        (value) =>
          [
            ...Object.values(sessionStorage),
            ...Object.values(localStorage),
          ].some((stored) => stored.includes(value)),
        secret,
      ),
    ).toBe(false);
    await row.getByRole("button", { name: "归档", exact: true }).click();
    const archive = page.getByRole("dialog", { name: "归档应用", exact: true });
    const archiveResponse = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname ===
          `/console/v1/admin/weixin-applications/${created.data.id}/archive` &&
        r.request().method() === "POST",
    );
    await archive
      .getByRole("button", { name: "确认归档", exact: true })
      .click();
    const archiveResult = await archiveResponse,
      archived = await archiveResult.json();
    expect(archiveResult.ok(), archived.message).toBeTruthy();
    expect(archived.data).toMatchObject({
      archived: true,
      enabled: false,
      execution_ready: false,
    });
    await expect(archive).toHaveCount(0);
    await expect(row).toContainText("已归档");
    expect(genericRequests).toEqual([]);
  });

  test("blacklist validation, create, edit, persisted detail and delete", async ({
    page,
  }) => {
    await page.goto("/admin/trade/blacklist");
    await page.getByRole("button", { name: "添加", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "添加", exact: true }).click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "拉黑类型", exact: true }).click();
    await page.getByRole("option", { name: /^(域名|domain)$/ }).click();
    await dialog
      .getByLabel("拉黑内容", { exact: true })
      .fill(tag + ".example.test");
    await dialog
      .getByLabel("备注", { exact: false })
      .fill("Local browser fixture");
    await dialog.getByRole("button", { name: "添加", exact: true }).click();
    const row = page.getByRole("row").filter({
      has: page.getByRole("cell", {
        name: tag + ".example.test",
        exact: true,
      }),
    });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await dialog.getByLabel("备注", { exact: false }).fill(tag + "-verified");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await page.reload();
    await row.getByRole("button", { name: "详情", exact: true }).click();
    await expect(dialog).toContainText(tag + "-verified");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await row.getByRole("button", { name: "删除", exact: true }).click();
    await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
    await expect(row).toHaveCount(0);
  });

  test("announcement rich text, merchant display, hide, edit and delete", async ({
    page,
    browser,
  }) => {
    await page.goto("/admin/system/anounce");
    await page.getByRole("button", { name: "添加公告", exact: true }).click();
    await page
      .locator('.rich-input[contenteditable="true"]')
      .fill(tag + "-notice");
    await page.getByRole("button", { name: "添加", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: /^添加公告成功$/ }),
    ).toBeVisible();
    const row = page
      .locator(".announcement-row")
      .filter({ hasText: tag + "-notice" });
    await expect(row).toBeVisible();
    const merchant = await browser.newContext({ baseURL: localSandboxBase() });
    try {
      const login = await merchant.request.post("/console/v1/merchant/login", {
        data: { username: "merchant", password: env.GOEPAY_SEED_PASSWORD },
      });
      expect(login.ok()).toBeTruthy();
      const dashboard = await merchant.newPage();
      await dashboard.goto("/merchant/dashboard");
      await expect(dashboard.locator("main")).toContainText(tag + "-notice");
      await row.getByRole("button", { name: "隐藏", exact: true }).click();
      await expect(
        row.getByRole("button", { name: "显示", exact: true }),
      ).toBeVisible();
      await dashboard.reload();
      await expect(dashboard.locator(".stat")).toHaveCount(4);
      await expect(dashboard.locator("main")).not.toContainText(
        tag + "-notice",
      );
    } finally {
      await merchant.close();
    }
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await page
      .locator('.rich-input[contenteditable="true"]')
      .fill(tag + "-edited-notice");
    await page
      .getByRole("region", { name: "编辑公告", exact: true })
      .getByRole("button", { name: /保存|修改/ })
      .click();
    const edited = page
      .locator(".announcement-row")
      .filter({ hasText: tag + "-edited-notice" });
    await expect(edited).toBeVisible();
    await edited.getByRole("button", { name: "删除", exact: true }).click();
    await page
      .getByRole("dialog", { name: "删除公告" })
      .getByRole("button", { name: "确定删除", exact: true })
      .click();
    await expect(edited).toHaveCount(0);
  });

  test("unpaid and paid order menus, status marker, income freeze and reversible delete", async ({
    page,
  }) => {
    const form: Record<string, string> = {
      pid: "1000",
      type: "sandbox",
      out_trade_no: tag + "-order-" + randomUUID(),
      name: "Local order actions",
      money: "0.10",
      sign_type: "MD5",
    };
    form.sign = createHash("md5")
      .update(
        Object.keys(form)
          .filter((k) => k !== "sign_type")
          .sort()
          .map((k) => k + "=" + form[k])
          .join("&") + env.GOEPAY_SEED_API_KEY,
      )
      .digest("hex");
    const created = await (await api.post("/mapi.php", { form })).json();
    expect(created.code).toBe(1);
    const trade = created.trade_no;
    const original = await call("/orders/" + trade);
    await page.goto("/admin/trade/order");
    const toggle = page.getByRole("button", {
      name: "订单操作 " + trade,
      exact: true,
    });
    await toggle.click();
    const menu = page.getByRole("menu", {
      name: "订单操作 " + trade,
      exact: true,
    });
    await expect(menu.getByRole("menuitem")).toHaveText([
      "改已完成",
      "手动补单",
      "删除订单",
    ]);
    await menu.getByRole("menuitem", { name: "改已完成", exact: true }).click();
    let dialog = page.getByRole("dialog", {
      name: "改已完成确认",
      exact: true,
    });
    await dialog.getByLabel("操作原因").fill("Synthetic marker only");
    await dialog.getByRole("button", { name: "确认修改状态" }).click();
    await expect(dialog).toContainText("后台状态标记已更新");
    const marked = await call("/orders/" + trade);
    expect(marked.order.status).toBe(original.order.status);
    expect(marked.admin_control.status).toBe("paid");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await call("/orders/" + trade + "/sandbox-paid", "POST", {});
    await page.reload();
    await toggle.click();
    await expect(menu.getByRole("menuitem")).toHaveText([
      "改未完成",
      "API退款",
      "手动退款",
      "冻结订单",
      "重新通知",
      "删除订单",
    ]);
    await menu.getByRole("menuitem", { name: "冻结订单", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "冻结订单确认", exact: true });
    await dialog
      .getByRole("button", { name: "确认冻结收入", exact: true })
      .click();
    await expect(
      dialog.getByRole("button", { name: "解冻订单收入", exact: true }),
    ).toBeVisible();
    const frozen = await call("/orders/" + trade);
    expect(frozen.income_hold.state).toBe("held");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await page.reload();
    await toggle.click();
    await menu.getByRole("menuitem", { name: "解冻订单", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "解冻订单确认", exact: true });
    await dialog
      .getByRole("button", { name: "确认解冻收入", exact: true })
      .click();
    await expect(
      dialog.getByRole("button", { name: "冻结订单收入", exact: true }),
    ).toBeVisible();
    expect((await call("/orders/" + trade)).income_hold.state).toBe("released");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await toggle.click();
    await menu.getByRole("menuitem", { name: "删除订单", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "删除订单确认" });
    await dialog.getByLabel("操作原因").fill("Hide own synthetic fixture");
    await dialog.getByRole("button", { name: "确认删除订单" }).click();
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await expect(toggle).toHaveCount(0);
    await page.getByLabel("显示已删除订单").check();
    await toggle.click();
    await menu.getByRole("menuitem", { name: "恢复订单", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "恢复订单确认" });
    await dialog.getByLabel("操作原因").fill("Restore own synthetic fixture");
    await dialog.getByRole("button", { name: "确认恢复订单" }).click();
    await expect(dialog).toContainText("订单已恢复显示");
    expect((await call("/orders/" + trade)).admin_control.archived).toBe(false);
  });
});
