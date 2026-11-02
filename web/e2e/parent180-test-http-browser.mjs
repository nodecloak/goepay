import { createServer } from "vite";
import { chromium, expect } from "@playwright/test";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const api = process.env.PARENT180_BROWSER_API,
  token = process.env.PARENT180_BROWSER_COOKIE,
  channel = process.env.PARENT180_BROWSER_CHANNEL,
  artifacts = process.env.GOEPAY_PARENT_CRUD_ARTIFACTS || process.env.PARENT180_BROWSER_ARTIFACTS;
if (!api?.startsWith("http://127.0.0.1:") || !token || !channel || !artifacts)
  throw new Error("explicit local synthetic browser fixture required");
await fs.mkdir(artifacts, { recursive: true });
const vite = await createServer({
  root: web,
  configFile: path.join(web, "vite.config.ts"),
  logLevel: "silent",
  server: {
    host: "127.0.0.1",
    port: 0,
    strictPort: true,
    proxy: {
      "/console": { target: api, changeOrigin: false },
      "/checkout": { target: api, changeOrigin: false },
    },
  },
});
let browser;
try {
  await vite.listen();
  const origin = "http://127.0.0.1:" + vite.httpServer.address().port;
  browser = await chromium.launch({ headless: true });
  const results = [];
  for (const width of [375, 1440]) {
    const context = await browser.newContext({
      viewport: { width, height: 900 },
    });
    const errors = [],
      external = [];
    await context.route("**/*", async (route) => {
      const u = new URL(route.request().url());
      if (!["127.0.0.1", "localhost"].includes(u.hostname)) {
        external.push(u.hostname);
        await route.abort();
        return;
      }
      await route.continue();
    });
    await context.addCookies([
      {
        name: "goepay_admin",
        value: token,
        url: origin,
        httpOnly: true,
        sameSite: "Lax",
      },
    ]);
    const page = await context.newPage();
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(origin + "/admin/payapi/pay-channel");
    await page.waitForLoadState("networkidle");
    await expect(
      page.getByRole("region", { name: "支付通道", exact: true }),
    ).toBeVisible();
    let configurationCRUD = false;
    if (process.env.GOEPAY_PARENT_CRUD_CERT_PATH && process.env.GOEPAY_PARENT_CRUD_KEY_PATH) {
      const name = "local-crud-" + width;
      await page.getByRole("button", { name: "新增通道", exact: true }).click();
      const editor = page.getByRole("dialog", { name: "父通道配置", exact: true });
      await editor.getByLabel("通道名称", { exact: true }).fill(name);
      await editor.getByLabel("分成比例", { exact: true }).fill("99");
      await editor.getByLabel("授权来源", { exact: true }).selectOption("8");
      await editor.getByLabel("appid 内容", { exact: true }).fill("wx-local-crud");
      await editor.getByLabel("appmchid 内容", { exact: true }).fill("local-crud-merchant");
      await editor.getByLabel("appkey 内容", { exact: true }).fill("0123456789abcdef0123456789abcdef");
      await editor.getByLabel("sslcert_path 内容", { exact: true }).fill(await fs.readFile(process.env.GOEPAY_PARENT_CRUD_CERT_PATH, "utf8"));
      await editor.getByLabel("sslkey_path 内容", { exact: true }).fill(await fs.readFile(process.env.GOEPAY_PARENT_CRUD_KEY_PATH, "utf8"));
      const creating = page.waitForResponse(r => r.request().method() === "POST" && new URL(r.url()).pathname === "/console/v1/admin/payment-channels");
      await editor.locator("[data-parent-save]").click();
      const created = await (await creating).json();
      expect(created.code).toBe(0);
      await expect(editor).toHaveCount(0);
      const createdID = created.data.id;
      const target = page.getByRole("row").filter({ has: page.getByRole("cell", { name: createdID, exact: true }) });
      await expect(target.getByRole("switch")).not.toBeChecked();
      await target.locator("[data-parent-parameters]").click();
      await editor.getByLabel("通道名称", { exact: true }).fill(name + "-edited");
      await editor.locator("[data-parent-save]").click();
      await expect(editor).toHaveCount(0);
      await page.reload();
      await expect(target.getByRole("cell", { name: name + "-edited", exact: true })).toBeVisible();
      for (const enabled of [true, false]) {
        await target.getByRole("switch").click();
        const confirm = page.getByRole("dialog", { name: "确认通道操作", exact: true });
        await confirm.getByRole("button", { name: "取消", exact: true }).click();
        await expect(target.getByRole("switch")).toBeChecked({ checked: !enabled });
        await target.getByRole("switch").click();
        await confirm.locator("[data-parent-confirm]").click();
        await expect(target.getByRole("switch")).toBeChecked({ checked: enabled });
      }
      await target.getByRole("button", { name: "删除", exact: true }).click();
      const confirm = page.getByRole("dialog", { name: "确认通道操作", exact: true });
      await confirm.locator("[data-parent-confirm]").click();
      await expect(target).toHaveCount(0);
      const retained = await context.request.get(origin + "/console/v1/admin/payment-channels/" + createdID);
      expect(retained.ok()).toBe(true);
      const detail = (await retained.json()).data;
      expect(detail.archived).toBe(true);
      expect(detail.account_version).toBe(created.data.account_version);
      configurationCRUD = true;
    }
    const row = page
      .getByRole("row")
      .filter({ has: page.getByRole("cell", { name: channel, exact: true }) });
    await page.screenshot({path:path.join(artifacts,"parent-channels-"+width+".png"),fullPage:true});
    await page.getByRole('button',{name:'显示列设置',exact:true}).click();
    await page.getByRole('checkbox',{name:'成本费率（估算）',exact:true}).check();
    await page.getByRole('checkbox',{name:'日限额、笔数及开放时间',exact:true}).check();
    await expect(page.getByRole('columnheader',{name:'每日限额',exact:true})).toBeVisible();
    await expect(row.getByRole('cell',{name:'全天',exact:true})).toBeVisible();
    await row.getByRole('button',{name:'通道 '+channel+' 更多操作',exact:true}).click();
    await page.getByRole('menuitem',{name:'高级设置',exact:true}).click();
    const advanced=page.getByRole('dialog',{name:'父通道限额设置',exact:true});
    await expect(advanced.getByRole('heading',{name:'父通道高级设置',exact:true})).toBeVisible();
    await advanced.getByRole('textbox',{name:'自定义订单名称',exact:true}).fill('[name]');
    await advanced.getByRole('textbox',{name:'按金额设置订单名称',exact:true}).fill('[]');
    await advanced.getByRole('button',{name:'保存订单名称',exact:true}).click();
    await advanced.locator('[data-subject-confirm]').click();
    await expect(advanced.getByText('订单名称设置已保存',{exact:true})).toBeVisible();
    await page.screenshot({path:path.join(artifacts,'parent-advanced-'+width+'.png'),fullPage:true});
    await advanced.getByRole('button',{name:'关闭',exact:true}).click();
    await expect(advanced).toHaveCount(0);
    await row.getByRole("button",{name:"测试",exact:true}).click();
    const modal = page.getByRole("dialog", { name: "测试支付", exact: true });
    await expect(modal).toBeVisible();
    await expect(modal.getByLabel("订单名称")).toHaveValue("支付测试");
    await expect(modal.getByLabel("订单金额")).toHaveValue("1");
    await modal.getByLabel("订单金额").fill("100.00");
    await modal.locator("[data-parent-test-submit]").click();
    await expect(modal.getByText("等待付款", { exact: true })).toBeVisible();
    await page.screenshot({
      path: path.join(artifacts, "parent-test-modal-" + width + ".png"),
      fullPage: true,
    });
    const [checkout] = await Promise.all([
      context.waitForEvent("page"),
      modal.getByRole("link", { name: "打开支付页面" }).click(),
    ]);
    await checkout.waitForLoadState("networkidle");
    await expect(checkout.locator("img").first()).toBeVisible();
    await checkout.screenshot({
      path: path.join(artifacts, "parent-test-checkout-" + width + ".png"),
      fullPage: true,
    });
    await checkout.close();
    await modal
      .getByRole("button", { name: "查询原订单", exact: true })
      .click();
    await expect(modal.getByText("已支付", { exact: true })).toBeVisible();
    await modal.getByRole("button", { name: "关闭", exact: true }).click();
    await expect(modal).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
    expect(external).toEqual([]);
    results.push({
      width,
      real_http: true,
      ordinary_first_send: true,
      protected_checkout: true,
      original_query_paid: true,
      original_subject_advanced_http: true,
      optional_real_limit_columns: true,
      configuration_crud: configurationCRUD,
      external_requests: 0,
      page_errors: 0,
    });
    await context.close();
  }
  await fs.writeFile(
    path.join(artifacts, "result.json"),
    JSON.stringify({ synthetic_only: true, viewports: results }, null, 2),
  );
  console.log(
    JSON.stringify({
      viewports: results.length,
      real_http: true,
      external_requests: 0,
    }),
  );
} finally {
  await browser?.close();
  await vite.close();
}
