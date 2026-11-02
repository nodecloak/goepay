import { test, expect } from "@playwright/test";
import { readLocalSandboxEnv, localSandboxBase } from "./local-sandbox-env";
test.use({ trace: "off" });
test("live local notification credentials and atomic template editing never dispatch external work", async ({
  page,
}, info) => {
  test.skip(
    process.env.GOEPAY_LIVE_NODE_WORKFLOWS_E2E !== "1",
    "Explicit owned local simulation only",
  );
  localSandboxBase();
  const env = Object.fromEntries(
    readLocalSandboxEnv(import.meta.url)
      .split(/\r?\n/)
      .filter(Boolean)
      .map((line) => {
        const i = line.indexOf("=");
        return [line.slice(0, i), line.slice(i + 1)];
      }),
  );
  const db = process.env.GOEPAY_LIVE_OWNED_DATABASE || "";
  if (
    !/^system229_[a-f0-9]{32}$/.test(db) ||
    new URL(env.DATABASE_URL).pathname !== "/" + db ||
    env.GOEPAY_PROVIDER_MODE !== "simulation" ||
    env.GOEPAY_ENABLE_PROVIDER_WRITES !== "false" ||
    env.GOEPAY_ENABLE_MERCHANT_NOTICES !== "false"
  )
    throw Error("Owned simulation required");
  await page.goto("/admin/login");
  await page.getByLabel("用户名", { exact: true }).fill("admin");
  await page.getByLabel("密码", { exact: true }).fill(env.GOEPAY_SEED_PASSWORD);
  await page.getByRole("button", { name: "登录工作台", exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/dashboard/);
  await page.goto("/admin/system/settings?tab=notice");
  const voice = page.getByRole("region", {
    name: "云喇叭语音播报",
    exact: true,
  });
  await voice.getByRole("button", { name: "凭据操作", exact: true }).click();
  await page
    .getByRole("option", { name: "填写或替换凭据", exact: true })
    .click();
  await voice
    .getByLabel("云喇叭agent_id", { exact: true })
    .fill("local-agent230");
  await voice
    .getByLabel("云喇叭agent_secret", { exact: true })
    .fill("local-secret230");
  await voice
    .getByRole("button", { name: "保存云喇叭配置", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(voice.getByRole("status")).toContainText("配置已保存");
  let reply = await page.request.get("/console/v1/admin/settings/voice");
  expect(reply.status()).toBe(200);
  let data = (await reply.json()).data;
  expect(data).toMatchObject({
    enabled: false,
    revision: "1",
    secret_present: true,
  });
  expect(JSON.stringify(data)).not.toContain("local-secret230");
  await voice.getByRole("button", { name: "凭据操作", exact: true }).click();
  await page
    .getByRole("option", { name: "清除凭据（先停用）", exact: true })
    .click();
  await voice
    .getByRole("button", { name: "保存云喇叭配置", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(voice.getByRole("status")).toContainText("配置已保存");
  reply = await page.request.get("/console/v1/admin/settings/voice");
  data = (await reply.json()).data;
  expect(data).toMatchObject({
    enabled: false,
    revision: "2",
    secret_present: false,
  });
  await page
    .getByLabel("微信订单支付成功 · 模板ID", { exact: true })
    .fill("local-template230");
  await page
    .getByLabel("微信订单支付成功 · 商品名称字段", { exact: true })
    .fill("thing1");
  await page
    .getByLabel("微信订单支付成功 · 金额字段", { exact: true })
    .fill("amount2");
  await page.getByRole("button", { name: "保存本页设置", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "已保存3项设置" }),
  ).toBeVisible();
  await page
    .getByLabel("微信订单支付成功 · 商品名称字段", { exact: true })
    .fill("amount2");
  await page
    .getByLabel("微信订单支付成功 · 金额字段", { exact: true })
    .fill("thing1");
  await page.getByRole("button", { name: "保存本页设置", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "已保存2项设置" }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("region", { name: "订单小票打印", exact: true })
      .getByLabel("APPSECRET", { exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(() =>
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ),
  ).not.toContain("local-secret230");
  await page.screenshot({
    path: info.outputPath("notice-config230.png"),
    fullPage: true,
  });
});
