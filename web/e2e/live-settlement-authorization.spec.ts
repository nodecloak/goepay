import { readLocalSandboxEnv } from "./local-sandbox-env";
import { test, expect, request } from "@playwright/test";
import crypto from "node:crypto";

test("live local settlement pays original net once after administrator funds confirmation", async ({
  page,
}) => {
  test.skip(
    process.env.GOEPAY_LIVE_FUNDS_E2E !== "1",
    "Separate serial local simulation acceptance only",
  );
  test.setTimeout(90000);
  const base = process.env.GOEPAY_WEB_URL || "";
  if (!/^http:\/\/(127\.0\.0\.1|localhost):\d+$/.test(base))
    throw new Error("Loopback URL required");
  const config = Object.fromEntries(
    readLocalSandboxEnv(import.meta.url)
      .split(/\r?\n/)
      .filter((x) => x && !x.startsWith("#"))
      .map((x) => {
        const at = x.indexOf("=");
        return [x.slice(0, at), x.slice(at + 1)];
      }),
  );
  const admin = await request.newContext({ baseURL: base });
  let csrf = "";
  const api = async (path: string, method = "GET", data?: unknown) => {
    const response = await admin.fetch("/console/v1/admin" + path, {
      method,
      data,
      headers: method === "GET" ? {} : { "X-CSRF-Token": csrf },
    });
    const body = await response.json();
    expect(response.ok(), body.message).toBeTruthy();
    expect(body.code, body.message).toBe(0);
    return body.data;
  };
  const restored: { id: string; value: string; created: boolean }[] = [];
  let operatorID = "",
    merchant: Record<string, unknown> | undefined;
  const username = `funds-live-${Date.now()}`,
    loginPassword = crypto.randomUUID() + "-login",
    fundsPassword = crypto.randomUUID() + "-funds";
  try {
    const login = await admin.post("/console/v1/admin/login", {
      data: { username: "admin", password: config.GOEPAY_SEED_PASSWORD },
    });
    const session = await login.json();
    expect(session.code).toBe(0);
    csrf = session.data.csrf_token;
    expect((await api("/dashboard")).sandbox).toBe(true);
    const settings = await api("/resources/settings?limit=100");
    if (
      settings.items.some(
        (v: Record<string, unknown>) =>
          v.key === "admin_funds_authorization_enabled" && v.value === "true",
      )
    )
      throw new Error("Refuse to override already-enforced local funds policy");
    const set = async (key: string, value: string) => {
      const previous = settings.items.find(
        (x: Record<string, unknown>) => x.key === key,
      );
      if (previous) {
        restored.push({
          id: String(previous.id),
          value: String(previous.value),
          created: false,
        });
        await api("/resources/settings/" + previous.id, "PATCH", { value });
      } else {
        const created = await api("/resources/settings", "POST", {
          key,
          value,
        });
        restored.push({ id: String(created.id), value: "", created: true });
      }
    };
    await set("transfer_enabled", "true");
    await set("settlement_minimum_cents", "0");
    await set("settlement_maximum_cents", "0");
    await set("settlement_fee_rate_bps", "250");
    await set("settlement_fee_minimum_cents", "0");
    await set("settlement_fee_maximum_cents", "0");
    await set("settlement_retained_cents", "0");
    await set("settlement_daily_limit", "0");
    await set("settle_type", "0");
    merchant = await api("/resources/merchants/1000");
    await api("/resources/merchants/1000", "PATCH", {
      transfer_api_enabled: true,
    });
    const recipient = await api(
      "/workflows/settlement-recipient?merchant_id=1000",
    );
    if (recipient.state === "missing")
      await api("/workflows/settlement-recipient", "POST", {
        merchant_id: "1000",
        type: "alipay",
        account: "synthetic-local-settlement",
        name: "Local Simulation Recipient",
        expected_config_id: "0",
      });
    const cap = await api("/transfers/capabilities?merchant_id=1000");
    expect(cap.channels.some((c: { id: string }) => c.id === "1")).toBe(true);
    const form: Record<string, string> = {
      pid: "1000",
      out_trade_no: "funds-live-" + crypto.randomUUID(),
      name: "Local settlement funds test",
      type: "sandbox",
      money: "1.00",
      sign_type: "MD5",
    };
    const canonical = Object.keys(form)
      .filter((k) => k !== "sign_type")
      .sort()
      .map((k) => `${k}=${form[k]}`)
      .join("&");
    form.sign = crypto
      .createHash("md5")
      .update(canonical + config.GOEPAY_SEED_API_KEY)
      .digest("hex");
    const apiBase = process.env.GOEPAY_API_URL || "http://127.0.0.1:8180";
    if (!/^http:\/\/(127\.0\.0\.1|localhost):\d+$/.test(apiBase))
      throw new Error("Loopback payment API URL required");
    const payment = await admin.post(apiBase + "/mapi.php", { form });
    const paid = await payment.json();
    expect(paid.code).toBe(1);
    await api("/orders/" + paid.trade_no + "/sandbox-paid", "POST", {});
    const external = "settlement-live-" + crypto.randomUUID();
    const current = await api(
      "/workflows/settlement-recipient?merchant_id=1000",
    );
    const application = await api("/workflows/settlements", "POST", {
      merchant_id: "1000",
      amount: "1.00",
      external_no: external,
      recipient_config_id: current.config_id,
      expected_fee_cents: "3",
      expected_net_cents: "97",
    });
    await api(`/workflows/settlements/${application.id}/approve`, "POST", {});
    const adminAccount = await api("/resources/admin-users", "POST", {
      username,
      display_name: "Local Funds Acceptance",
      password: loginPassword,
      admin_class: "operator",
    });
    operatorID = String(adminAccount.id);
    await page.goto("/admin/dashboard");
    await page.getByLabel("用户名", { exact: true }).fill(username);
    await page.getByLabel("密码", { exact: true }).fill(loginPassword);
    await page.getByRole("button", { name: "登录工作台" }).click();
    await expect(page.locator(".stat")).toHaveCount(4);
    await page.goto("/admin/system/change-password");
    const settingsPanel = page.getByRole("region", { name: "管理员资金密码" });
    await settingsPanel
      .getByRole("button", { name: "设置资金密码", exact: true })
      .click();
    await settingsPanel
      .getByLabel("当前管理员登录密码", { exact: true })
      .fill(loginPassword);
    await settingsPanel
      .getByLabel("新资金密码", { exact: true })
      .fill(fundsPassword);
    await settingsPanel
      .getByLabel("确认新资金密码", { exact: true })
      .fill(fundsPassword);
    await settingsPanel
      .getByRole("button", { name: "确认更新资金密码" })
      .click();
    await expect(settingsPanel).toContainText("已设置本人资金密码");
    // This is the only test that enables the global local policy. Run serially
    // after the ordinary live suite; restore it before fixture cleanup.
    await set("admin_funds_authorization_enabled", "true");
    await page.goto("/admin/settle/list");
    const row = page.locator(`[data-settlement-id="${application.id}"]`);
    await row.getByRole("button", { name: "核对结算出款" }).click();
    const panel = page.getByRole("region", { name: "结算出款", exact: true });
    await panel.getByLabel("出款通道").selectOption("1");
    await panel
      .getByRole("button", { name: "准备唯一出款绑定", exact: true })
      .click();
    await panel
      .getByRole("button", { name: "确认此操作", exact: true })
      .click();
    await expect(
      panel.getByRole("button", { name: "发送原结算出款", exact: true }),
    ).toBeEnabled();
    await panel
      .getByRole("button", { name: "发送原结算出款", exact: true })
      .click();
    await panel
      .getByRole("button", { name: "确认发送并核对资金授权", exact: true })
      .click();
    const modal = page.getByRole("dialog", { name: "管理员资金二次授权" });
    await expect(modal).toContainText("0.97");
    await expect(modal).toContainText("0.03");
    await modal
      .getByRole("button", { name: "已核对摘要，验证资金密码" })
      .click();
    await modal
      .getByLabel("本人管理员资金密码", { exact: true })
      .fill(fundsPassword);
    await modal
      .getByRole("button", { name: "确认资金密码", exact: true })
      .click();
    await modal.getByRole("button", { name: "执行已核对的原操作" }).click();
    await expect(panel).toContainText("已核实出款成功");
    await expect(
      panel.getByRole("button", { name: "发送原结算出款", exact: true }),
    ).toHaveCount(0);
    const read = await api("/settlement-payouts/" + application.id);
    expect(read.payout.state).toBe("succeeded");
    expect(read.payout.transfer.amount).toBe("97");
    expect(read.payout.transfer.fee_amount).toBe("0");
    expect(read.payout.funding.state).toBe("paid");
    expect(
      await page.evaluate(() =>
        Object.keys(localStorage).concat(Object.keys(sessionStorage)),
      ),
    ).toEqual([]);
    expect(await page.content()).not.toContain(fundsPassword);
  } finally {
    for (const item of restored.reverse()) {
      if (item.created) await api("/resources/settings/" + item.id, "DELETE");
      else
        await api("/resources/settings/" + item.id, "PATCH", {
          value: item.value,
        });
    }
    if (merchant)
      await api("/resources/merchants/1000", "PATCH", {
        transfer_api_enabled: merchant.transfer_api_enabled === true,
      });
    if (operatorID)
      await api("/resources/admin-users/" + operatorID, "PATCH", {
        enabled: false,
      });
    await admin.dispose();
  }
});
