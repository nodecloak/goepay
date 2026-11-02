import { test, expect } from "@playwright/test";

test.use({
  trace: "off",
  viewport: {
    width: Number(process.env.GOEPAY_MIGRATION234_WIDTH || 1280),
    height: 900,
  },
});
test("local real API migrates a SQL backup once and preserves a safe activation boundary", async ({
  page,
}) => {
  test.skip(
    process.env.GOEPAY_MIGRATION234_LIVE !== "1",
    "Owned loopback migration fixture only",
  );
  const base = process.env.GOEPAY_WEB_URL || "";
  expect(new URL(base).hostname).toBe("127.0.0.1");
  expect(process.env.GOEPAY_LIVE_OWNED_DATABASE).toMatch(
    /^migration234_[a-f0-9]{32}$/,
  );
  const password = process.env.GOEPAY_SEED_PASSWORD;
  expect(password?.length).toBeGreaterThanOrEqual(24);
  await page.route("**/*", (route) =>
    new URL(route.request().url()).origin === base
      ? route.continue()
      : route.abort(),
  );
  const login = await page.request.post(base + "/console/v1/admin/login", {
    data: { username: "admin", password },
  });
  expect(login.ok()).toBeTruthy();
  const session = await login.json();
  expect(session.code).toBe(0);
  const csrf = session.data.csrf_token;
  const sql =
    "CREATE TABLE pay_user(uid bigint,money decimal(10,2),codename text,addtime datetime,deposit decimal(10,2)); INSERT INTO pay_user VALUES(9001,12.50,'Synthetic merchant','2026-10-01 00:00:00',0);" +
    "CREATE TABLE pay_type(id int,name text); INSERT INTO pay_type VALUES(1,'alipay');" +
    "CREATE TABLE pay_order(trade_no text,uid int,out_trade_no text,name text,type int,money decimal(10,2),realmoney decimal(10,2),status int,addtime datetime,endtime datetime,tid int,version int); INSERT INTO pay_order VALUES('2340000000000000001',9001,'synthetic234','Synthetic Product',1,1.00,1.00,1,'2026-10-01 01:00:00','2026-10-01 01:01:00',0,0);" +
    "CREATE TABLE pay_config(k text,v text); INSERT INTO pay_config VALUES('secret','synthetic-not-for-ui234');";
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  let prepares = 0,
    applies = 0;
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().endsWith("/migration/epay/prepare"))
      prepares++;
    if (r.method() === "POST" && r.url().endsWith("/apply")) applies++;
  });
  await page.goto(base + "/admin/system/migration");
  await expect(
    page.getByRole("region", { name: "易支付迁移向导", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "选择SQL备份", exact: true }),
  ).toBeEnabled();
  await page
    .getByLabel("易支付SQL备份", { exact: true })
    .setInputFiles({
      name: "synthetic.sql",
      mimeType: "application/sql",
      buffer: Buffer.from(sql),
    });
  await page.getByRole("button", { name: "上传并预检查", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "读取当前任务状态", exact: true }),
  ).toBeVisible();
  const runs = await (
    await page.request.get(base + "/console/v1/admin/migration/epay")
  ).json();
  const task = runs.data.runs[0];
  expect(task.status).toBe("prepared");
  expect(task.issues).toEqual([]);
  expect(task.available_total).toBe("12.50");
  expect(task.table_counts).toContainEqual({
    name: "pay_config",
    rows: 1,
    mode: "archive",
  });
  await expect(
    page.getByText("synthetic-not-for-ui234", { exact: false }),
  ).toHaveCount(0);
  const apply = page.getByRole("button", { name: "开始导入", exact: true });
  await expect(apply).toBeDisabled();
  await page.getByLabel("确认源站停止写入且备份一致", { exact: true }).check();
  await page.getByLabel("确认仅归档数据不会自动启用", { exact: true }).check();
  await page.getByLabel("输入MIGRATE确认导入", { exact: true }).fill("MIGRATE");
  await expect(apply).toBeEnabled();
  await apply.click();
  await expect(
    page.getByRole("heading", { name: "导入完成，待配置和启用", exact: true }),
  ).toBeVisible();
  expect(prepares).toBe(1);
  expect(applies).toBe(1);
  const result = await (
    await page.request.get(base + "/console/v1/admin/migration/epay/" + task.id)
  ).json();
  expect(result.data.report).toMatchObject({
    valid: true,
    applied: true,
    merchant_count: 1,
    order_count: 1,
    available_total: "12.50",
  });
  const replay = await page.request.post(
    base + "/console/v1/admin/migration/epay/" + task.id + "/apply",
    {
      headers: { "X-CSRF-Token": csrf },
      data: {
        expected_sha256: task.sha256,
        source_frozen: true,
        archive_acknowledged: true,
        confirmation: "MIGRATE",
      },
    },
  );
  expect(replay.ok()).toBeTruthy();
  expect((await replay.json()).data.status).toBe("completed");
  await page.reload();
  await expect(page.getByText(/当前实例已有业务数据/)).toBeVisible();
  await page.screenshot({
    path: process.env.GOEPAY_MIGRATION234_SCREENSHOT,
    fullPage: true,
  });
  expect(errors).toEqual([]);
});
