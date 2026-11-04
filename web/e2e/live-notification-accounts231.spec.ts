import { test, expect } from "@playwright/test";
import { createPrivateKey, sign } from "node:crypto";
import { readLocalSandboxEnv, localSandboxBase } from "./local-sandbox-env";
test.use({ trace: "off" });

test("live local administrator bot editing, signed one-shot binding and unbinding", async ({
  page,
}) => {
  test.skip(
    process.env.GOEPAY_LIVE_NODE_WORKFLOWS_E2E !== "1",
    "Explicit local simulation only",
  );
  localSandboxBase();
  const env = Object.fromEntries(
    readLocalSandboxEnv(import.meta.url)
      .split(/\r?\n/)
      .filter(Boolean)
      .map((l) => {
        const n = l.indexOf("=");
        return [l.slice(0, n), l.slice(n + 1)];
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
  const robot = page.getByRole("region", {
    name: "管理员群机器人配置",
    exact: true,
  });
  await robot
    .getByRole("button", { name: "设置管理员群机器人配置凭据", exact: true })
    .click();
  await robot.getByLabel("管理员手机号码", { exact: true }).fill("13800138231");
  await robot
    .getByRole("button", { name: "保存管理员群机器人配置", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(robot.getByRole("status")).toContainText("配置已保存");
  const phoneView = (
    await (
      await page.request.get("/console/v1/admin/settings/notice-accounts/robot")
    ).json()
  ).data;
  expect(phoneView).toMatchObject({
    enabled: false,
    secret_present: false,
    revision: "1",
  });
  expect(JSON.stringify(phoneView)).not.toContain("13800138231");
  const qq = page.getByRole("region", {
    name: "管理员QQ机器人配置",
    exact: true,
  });
  await qq
    .getByRole("button", { name: "设置管理员QQ机器人配置凭据", exact: true })
    .click();
  await qq.getByLabel("管理员QQ机器人AppID", { exact: true }).fill("123456231");
  const secret = "synthetic-live-secret231";
  await qq.getByLabel("管理员QQ机器人AppSecret", { exact: true }).fill(secret);
  await qq
    .getByRole("switch", { name: "管理员QQ机器人配置开关", exact: true })
    .check();
  await qq
    .getByRole("button", { name: "保存管理员QQ机器人配置", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(
    qq.getByRole("status").filter({ hasText: "配置已保存" }),
  ).toBeVisible();
  await qq.getByRole("button", { name: "开始QQ绑定", exact: true }).click();
  const code = await qq.locator(".bot-binding231 code").textContent();
  expect(code).toMatch(/^GOEPAY-[0-9a-f]{20}$/);
  const key = createPrivateKey({
    key: Buffer.concat([
      Buffer.from("302e020100300506032b657004220420", "hex"),
      Buffer.from(secret.repeat(3)).subarray(0, 32),
    ]),
    format: "der",
    type: "pkcs8",
  });
  async function callback(recipient: string) {
    const body = JSON.stringify({
      op: 0,
      t: "C2C_MESSAGE_CREATE",
      id: "synthetic-event231",
      d: { content: code, author: { user_openid: recipient } },
    });
    const stamp = String(Math.floor(Date.now() / 1000));
    return page.request.post("/payment/qqbot/webhook", {
      data: body,
      headers: {
        "Content-Type": "application/json",
        "X-Bot-Appid": "123456231",
        "X-Signature-Timestamp": stamp,
        "X-Signature-Ed25519": sign(
          null,
          Buffer.from(stamp + body),
          key,
        ).toString("hex"),
      },
    });
  }
  expect((await callback("synthetic-recipient231")).status()).toBe(200);
  await expect(qq.getByText(/已绑定 \*\*\*/)).toBeVisible();
  expect((await callback("other-recipient231")).status()).toBe(200);
  let view = (
    await (
      await page.request.get("/console/v1/admin/settings/notice-accounts/qqbot")
    ).json()
  ).data;
  expect(view).toMatchObject({ bound: true, revision: "2" });
  expect(JSON.stringify(view)).not.toContain(secret);
  await qq.getByRole("button", { name: "解除QQ绑定", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认解绑", exact: true })
    .click();
  await expect(
    qq.getByRole("status").filter({ hasText: "QQ绑定已解除" }),
  ).toBeVisible();
  view = (
    await (
      await page.request.get("/console/v1/admin/settings/notice-accounts/qqbot")
    ).json()
  ).data;
  expect(view).toMatchObject({ bound: false, revision: "3" });
  expect((await callback("other-recipient231")).status()).toBe(200);
  view = (
    await (
      await page.request.get("/console/v1/admin/settings/notice-accounts/qqbot")
    ).json()
  ).data;
  expect(view).toMatchObject({ bound: false, revision: "3" });
  await page.getByRole("tab", { name: "短信配置", exact: true }).click();
  const sms = page.getByRole("region", { name: "业务短信模板", exact: true });
  await expect(sms.locator("input")).toHaveCount(9);
  await expect(
    page
      .locator("#settings-panel-sms")
      .getByText("该分类当前没有可在线维护的设置项。", { exact: true }),
  ).toHaveCount(0);
});
