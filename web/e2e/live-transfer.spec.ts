import { readLocalSandboxEnv } from "./local-sandbox-env";
import { test, expect, request } from "@playwright/test";
test("live local wallet transfer follows capability, confirmation and readonly query with restored settings", async ({
  page,
}) => {
  test.skip(process.env.GOEPAY_LIVE_E2E !== "1", "Local seeded sandbox only");
  test.setTimeout(60000);
  const value = readLocalSandboxEnv(import.meta.url)
    .split(/\r?\n/)
    .find((x) => x.startsWith("GOEPAY_SEED_PASSWORD="))
    ?.slice("GOEPAY_SEED_PASSWORD=".length);
  if (!value) throw new Error("Missing local seed password");
  const admin = await request.newContext({
    baseURL: process.env.GOEPAY_WEB_URL,
  });
  let csrf = "";
  async function api(path: string, method = "GET", data?: unknown) {
    const r = await admin.fetch("/console/v1/admin" + path, {
      method,
      data,
      headers: method === "GET" ? {} : { "X-CSRF-Token": csrf },
    });
    const b = await r.json();
    expect(r.ok(), b.message).toBeTruthy();
    expect(b.code, b.message).toBe(0);
    return b.data;
  }
  let merchant: Record<string, unknown> | undefined,
    setting: Record<string, unknown> | undefined,
    createdSetting: string | undefined,
    touched = false;
  try {
    const login = await admin.post("/console/v1/admin/login", {
      data: { username: "admin", password: value },
    });
    const session = await login.json();
    expect(session.code).toBe(0);
    csrf = session.data.csrf_token;
    expect((await api("/dashboard")).sandbox).toBe(true);
    merchant = await api("/resources/merchants/1000");
    const settings = await api(
      "/resources/settings?q=transfer_enabled&limit=100",
    );
    setting = settings.items.find(
      (x: Record<string, unknown>) => x.key === "transfer_enabled",
    );
    touched = true;
    await api("/resources/merchants/1000", "PATCH", {
      transfer_api_enabled: true,
      transfer_rate: "3",
    });
    if (setting)
      await api("/resources/settings/" + setting.id, "PATCH", {
        value: "true",
      });
    else
      createdSetting = String(
        (
          await api("/resources/settings", "POST", {
            key: "transfer_enabled",
            value: "true",
          })
        ).id,
      );
    const cap = await api("/transfers/capabilities?merchant_id=1000");
    expect(cap.writes_enabled).toBe(true);
    expect(
      cap.channels.some(
        (c: { id: string; types: string[] }) =>
          c.id === "1" && c.types.includes("alipay"),
      ),
    ).toBe(true);
    await page.goto("/merchant/dashboard");
    await page.getByLabel("用户名", { exact: true }).fill("merchant");
    await page.getByLabel("密码", { exact: true }).fill(value);
    await page.getByRole("button", { name: "登录工作台" }).click();
    await expect(page.locator(".stat")).toHaveCount(4);
    await page.goto("/merchant/transfer");
    await page.getByRole("button", { name: "申请付款", exact: true }).click();
    const modal = page.getByRole("dialog", { name: "申请付款" });
    await modal.getByLabel("收款方式").selectOption("alipay");
    await modal.getByLabel("付款通道").selectOption("1");
    await modal.getByLabel("收款账号").fill("local-simulation@example.test");
    await modal.getByLabel("收款人姓名").fill("Local simulation");
    await modal.getByLabel("付款金额（元）").fill("0.01");
    await modal
      .getByLabel("付款说明", { exact: true })
      .fill("Local simulation browser test");
    await modal.getByLabel("业务请求号").fill("live-wallet-" + Date.now());
    await modal.getByRole("button", { name: "核对付款信息" }).click();
    await expect(modal).toContainText("3%");
    const submitted = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname.endsWith("/transfers") &&
        r.request().method() === "POST",
    );
    await modal.getByRole("button", { name: "确认提交付款" }).click();
    const transferResponse = await submitted;
    expect(transferResponse.ok()).toBeTruthy();
    const transferBody = await transferResponse.json();
    expect(transferBody.code).toBe(0);
    const originalTransferID = String(transferBody.data.id);
    expect(originalTransferID).toMatch(/^[1-9][0-9]*$/);
    const detail = page.getByRole("dialog", { name: "付款详情" });
    await expect(detail).toContainText("通道已确认付款成功");
    await expect(detail).toContainText("local-simulation@example.test");
    // Same seeded local simulation and existing sessions: prepare is visibly
    // separate from execute, and cancellation releases only the remaining item.
    await page.goto("/merchant/transfer/batches");
    await page
      .getByRole("button", { name: "新建批量付款", exact: true })
      .click();
    await page
      .getByRole("combobox", { name: "通道", exact: true })
      .selectOption("1");
    await page
      .getByLabel("收款账号", { exact: true })
      .fill("local-batch@example.test");
    await page.getByLabel("付款金额（元）", { exact: true }).fill("0.01");
    await page
      .getByRole("button", { name: "添加付款项目", exact: true })
      .click();
    const second = page.getByRole("group", { name: "付款项目 2", exact: true });
    await second
      .getByRole("combobox", { name: "通道", exact: true })
      .selectOption("1");
    await second
      .getByLabel("收款账号", { exact: true })
      .fill("local-batch@example.test");
    await second.getByLabel("付款金额（元）", { exact: true }).fill("0.01");
    await page
      .getByRole("button", { name: "核对并预留整批资金", exact: true })
      .click();
    await page
      .getByRole("button", { name: "确认预留，不发送付款", exact: true })
      .click();
    const batch = page.getByRole("region", {
      name: "批量付款详情",
      exact: true,
    });
    await expect(
      batch.getByRole("cell", { name: "unsent", exact: true }),
    ).toHaveCount(4);
    await batch
      .getByRole("button", { name: "执行尚未发送项目", exact: true })
      .click();
    await batch.getByLabel("本轮最多执行项目数").fill("1");
    await batch
      .getByRole("button", { name: "确认仅执行未发送项目", exact: true })
      .click();
    await expect(batch.locator('[data-state="succeeded"]')).toHaveCount(2);
    await batch
      .getByRole("button", { name: "取消尚未发送项目", exact: true })
      .click();
    await batch
      .getByRole("button", { name: "确认取消未发送项目", exact: true })
      .click();
    await expect(batch.locator('[data-state="cancelled"]')).toHaveCount(2);
    await expect(
      batch.getByRole("button", { name: "执行尚未发送项目", exact: true }),
    ).toHaveCount(0);
    await api("/resources/merchants/1000", "PATCH", {
      transfer_api_enabled: false,
    });
    await page.goto("/merchant/transfer");
    // Query the original single transfer after the write switch is stopped.
    await page
      .locator(`[data-transfer-id="${originalTransferID}"]`)
      .getByRole("button", { name: "付款详情", exact: true })
      .click();
    await detail.getByRole("button", { name: "查询通道付款结果" }).click();
    await expect(detail).toContainText("通道已确认付款成功");
  } finally {
    const restorationErrors: string[] = [];
    try {
      if (touched && merchant)
        await api("/resources/merchants/1000", "PATCH", {
          transfer_api_enabled: merchant.transfer_api_enabled ?? false,
          transfer_rate: merchant.transfer_rate ?? "",
        });
    } catch {
      restorationErrors.push(
        "Merchant transfer configuration restoration failed",
      );
    }
    try {
      if (setting)
        await api("/resources/settings/" + setting.id, "PATCH", {
          value: setting.value,
        });
      else if (createdSetting)
        await api("/resources/settings/" + createdSetting, "DELETE");
    } catch {
      restorationErrors.push(
        "Platform transfer configuration restoration failed",
      );
    }
    await admin.dispose();
    expect(restorationErrors).toEqual([]);
  }
});
