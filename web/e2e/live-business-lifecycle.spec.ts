import { test, expect, type APIRequestContext } from "@playwright/test";
import { createHash, randomUUID } from "node:crypto";
import { localSandboxBase, readLocalSandboxEnv } from "./local-sandbox-env";

// Real HTTP, database and browser sessions; never usable against a public host.
test("new merchant lifecycle: group, login, signed payments, callbacks, refunds and settlement", async ({
  browser,
  playwright,
}) => {
  test.skip(
    process.env.GOEPAY_LIVE_E2E !== "1" ||
      process.env.GOEPAY_LIVE_FUNDS_E2E !== "1",
    "Explicit local simulation acceptance only",
  );
  test.setTimeout(180_000);
  const base = localSandboxBase();
  expect(process.env.GOEPAY_LIVE_ENV_FILE).toBeTruthy();
  expect(process.env.GOEPAY_LIVE_OWNED_DATABASE).toMatch(
    /^businessflow225_20261102_[a-f0-9]{32}$/,
  );
  const env = Object.fromEntries(
    readLocalSandboxEnv(import.meta.url)
      .split(/\r?\n/)
      .filter(Boolean)
      .map((line) => {
        const at = line.indexOf("=");
        return [line.slice(0, at), line.slice(at + 1)];
      }),
  );
  expect(env.GOEPAY_SANDBOX).toBe("true");
  expect(env.GOEPAY_PROVIDER_MODE).toBe("simulation");
  expect(env.GOEPAY_ENABLE_PROVIDER_WRITES).toBe("false");
  expect(env.GOEPAY_PUBLIC_ORIGIN).toBe(base);
  const database = new URL(env.DATABASE_URL!);
  expect(database.hostname).toBe("127.0.0.1");
  expect(database.port).toBe("55101");
  expect(database.pathname).toBe("/" + process.env.GOEPAY_LIVE_OWNED_DATABASE);
  expect(env.GOEPAY_SIMULATION_MERCHANT_IDS).toBe("1000,10000");
  const admin = await browser.newContext({ baseURL: base });
  const merchant = await browser.newContext({ baseURL: base });
  const other = await playwright.request.newContext({ baseURL: base });
  const checkpoints: Record<string, unknown>[] = [];
  const errors: string[] = [];
  for (const context of [admin, merchant]) {
    context.setDefaultTimeout(15_000);
    context.setDefaultNavigationTimeout(15_000);
    await context.route("**/*", (route) =>
      new URL(route.request().url()).origin === base
        ? route.continue()
        : route.abort(),
    );
    context.on("page", (page) =>
      page.on("pageerror", (error) => errors.push(error.message)),
    );
  }
  let adminCSRF = "",
    merchantCSRF = "";
  async function call(
    client: APIRequestContext,
    area: string,
    csrf: string,
    path: string,
    method = "GET",
    data?: unknown,
  ) {
    const response = await client.fetch(`/console/v1/${area}${path}`, {
      method,
      data,
      maxRedirects: 0,
      headers: method === "GET" ? {} : { "X-CSRF-Token": csrf },
    });
    const body = await response.json();
    expect(response.ok(), `${path}: ${body.message}`).toBeTruthy();
    expect(body.code, `${path}: ${body.message}`).toBe(0);
    return body.data;
  }
  const a = (path: string, method = "GET", data?: unknown) =>
    call(admin.request, "admin", adminCSRF, path, method, data);
  const m = (path: string, method = "GET", data?: unknown) =>
    call(merchant.request, "merchant", merchantCSRF, path, method, data);
  const tag = randomUUID();
  const email = `lifecycle+${tag}@example.invalid`,
    password = `Local-${randomUUID()}`;
  try {
    const login = await admin.request.post("/console/v1/admin/login", {
      data: { username: "admin", password: env.GOEPAY_SEED_PASSWORD },
    });
    expect(login.ok()).toBeTruthy();
    const session = await login.json();
    expect(session.code).toBe(0);
    adminCSRF = session.data.csrf_token;
    expect((await a("/dashboard")).sandbox).toBe(true);
    // The runner owns and drops this entire database; policies never leave it.
    const settings = await a("/resources/settings?limit=100");
    for (const [key, value] of Object.entries({
      payment_fee_enabled: "true",
      refund_fee_payer: "platform",
      settle_type: "0",
      transfer_enabled: "true",
      settlement_minimum_cents: "0",
      settlement_maximum_cents: "0",
      settlement_fee_rate_bps: "250",
      settlement_fee_minimum_cents: "0",
      settlement_fee_maximum_cents: "0",
      settlement_retained_cents: "0",
      settlement_daily_limit: "0",
    })) {
      const existing = settings.items.find(
        (row: { key: string }) => row.key === key,
      );
      await a(
        "/resources/settings" + (existing ? `/${existing.id}` : ""),
        existing ? "PATCH" : "POST",
        existing ? { value } : { key, value },
      );
    }
    const ap = await admin.newPage(),
      mp = await merchant.newPage();
    await test.step("admin creates a group and contact merchant through the UI", async () => {
      await ap.goto("/admin/merchant/group-list");
      await ap.getByRole("button", { name: "新增用户组", exact: true }).click();
      let dialog = ap.getByRole("dialog");
      await dialog
        .getByLabel("显示名称", { exact: false })
        .fill(`Lifecycle ${tag}`);
      await dialog.getByLabel("费率（万分比）", { exact: true }).fill("200");
      await dialog
        .getByLabel("支付宝通道", { exact: true })
        .selectOption("default");
      await dialog
        .getByLabel("微信支付通道", { exact: true })
        .selectOption("default");
      const groupResponse = ap.waitForResponse(
        (r) =>
          new URL(r.url()).pathname === "/console/v1/admin/resources/groups" &&
          r.request().method() === "POST",
      );
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      const group = await (await groupResponse).json();
      expect(group.code, group.message).toBe(0);
      const groupID = group.data.id;
      await a(`/resources/groups/${groupID}`, "PATCH", {
        settle_type: "0",
        settle_open: "2",
        settle_minmoney: "0",
        settle_rate: "2.5",
        settle_alipay: "1",
      });
      await ap.goto("/admin/merchant/merchant-list");
      await ap.getByRole("button", { name: "新增商户", exact: true }).click();
      dialog = ap.getByRole("dialog");
      await dialog.getByLabel("邮箱(登录账号)", { exact: true }).fill(email);
      await dialog.getByLabel("登录密码", { exact: true }).fill(password);
      await expect(
        dialog
          .getByLabel("用户组", { exact: true })
          .locator(`option[value="${groupID}"]`),
      ).toHaveCount(1);
      await dialog.getByLabel("用户组", { exact: true }).selectOption(groupID);
      const createdResponse = ap.waitForResponse(
        (r) =>
          new URL(r.url()).pathname ===
            "/console/v1/admin/resources/merchants" &&
          r.request().method() === "POST",
      );
      await dialog
        .getByRole("button", { name: "确定添加", exact: true })
        .click();
      const created = await (await createdResponse).json();
      expect(created.code, created.message).toBe(0);
      const merchantID = created.data.id;
      expect(merchantID).toBe("10000");
      expect(
        String((await a(`/resources/merchants/${merchantID}`)).group_id),
      ).toBe(groupID);
      const state = await a(`/merchants/${merchantID}/credentials`);
      const credential = await a(
        `/merchants/${merchantID}/credentials/reset-md5`,
        "POST",
        { expected_fingerprint: state.fingerprint },
      );
      expect(credential.api_key).toMatch(/^[a-f0-9]{32}$/);
      await mp.goto("/merchant/login");
      await mp.getByLabel("用户名", { exact: true }).fill(email);
      await mp.getByLabel("密码", { exact: true }).fill(password);
      await mp.getByRole("button", { name: "登录工作台", exact: true }).click();
      await expect(mp).toHaveURL(/\/merchant\/dashboard$/);
      const own = await m("/session");
      expect(own.user).toMatchObject({ id: merchantID, role: "merchant" });
      expect(own.delegated_by).toBeFalsy();
      merchantCSRF = own.csrf_token;
      expect((await m("/dashboard")).available_money).toBe("0.00");
      checkpoints.push({
        stage: "created",
        merchant_id: merchantID,
        group_id: groupID,
        available: "0.00",
      });

      const trades: Record<string, string> = {};
      await test.step("WeChat and Alipay signed requests and verified callback replay", async () => {
        for (const [kind, money, net, fee] of [
          ["wxpay", "10.00", "980", "20"],
          ["alipay", "20.00", "1960", "40"],
        ]) {
          const form: Record<string, string> = {
            pid: merchantID,
            type: kind!,
            out_trade_no: `${tag}-${kind}`,
            name: "Local connected lifecycle",
            money: money!,
            // No notification worker is started; reserved .invalid names cannot
            // route to a real merchant. Loopback callbacks are correctly denied.
            notify_url: "https://lifecycle.example.invalid/notify",
            return_url: "https://lifecycle.example.invalid/return",
            sign_type: "MD5",
          };
          form.sign = createHash("md5")
            .update(
              Object.keys(form)
                .filter((k) => k !== "sign_type")
                .sort()
                .map((k) => `${k}=${form[k]}`)
                .join("&") + credential.api_key,
            )
            .digest("hex");
          const createdPayment = await merchant.request.post("/mapi.php", {
            form,
            maxRedirects: 0,
          });
          const payment = await createdPayment.json();
          expect(payment.code, payment.msg).toBe(1);
          trades[kind!] = payment.trade_no;
          expect(new URL(payment.payurl).origin).toBe(base);
          const checkout = await merchant.request.get(payment.payurl, {
            maxRedirects: 0,
          });
          expect(checkout.ok()).toBeTruthy();
          const simulationURL = (await checkout.text()).match(
            /http:\/\/(?:127\.0\.0\.1|localhost):\d+\/simulation\/pay\?token=[A-Za-z0-9_.%~-]+/,
          )?.[0];
          expect(simulationURL).toBeTruthy();
          const url = new URL(simulationURL!);
          expect(url.origin).toBe(base);
          const [encoded, mac] = url.searchParams.get("token")!.split(".");
          const record = JSON.parse(
            Buffer.from(encoded!, "base64url").toString(),
          );
          const callback = `/callbacks/providers/${record.channel_id}/${payment.trade_no}`;
          const invalid = await merchant.request.post(callback, {
            data: { record: { ...record, amount: record.amount + 1 }, mac },
            maxRedirects: 0,
          });
          expect(invalid.status()).toBe(400);
          expect((await m(`/orders/${payment.trade_no}`)).order.status).toBe(
            "pending",
          );
          expect(
            (
              await merchant.request.post(simulationURL!, { maxRedirects: 0 })
            ).ok(),
          ).toBeTruthy();
          for (const reply of await Promise.all(
            Array.from({ length: 3 }, () =>
              merchant.request.post(callback, {
                data: { record, mac },
                maxRedirects: 0,
              }),
            ),
          ))
            expect(reply.ok()).toBeTruthy();
          const order = (await m(`/orders/${payment.trade_no}`)).order;
          expect(order).toMatchObject({
            status: "paid",
            merchant_amount: net,
            platform_fee: fee,
            fee_rate_bps: 200,
            fee_group_id: groupID,
          });
          const ledger = await m(`/ledger?q=${payment.trade_no}`);
          expect(ledger.total).toBe(1);
          expect(ledger.items[0].amount).toBe(net);
          const adminOrder = (await a(`/orders/${payment.trade_no}`)).order;
          expect(adminOrder.status).toBe("paid");
        }
        expect((await m("/dashboard")).available_money).toBe("29.40");
        checkpoints.push({
          stage: "paid",
          available: "29.40",
          orders: trades,
          callback_replays: 6,
        });
      });

      await test.step("cross merchant isolation and partial then final refund", async () => {
        const otherLogin = await other.post("/console/v1/merchant/login", {
          data: { username: "merchant", password: env.GOEPAY_SEED_PASSWORD },
        });
        const otherSession = await otherLogin.json();
        expect(otherSession.code).toBe(0);
        const denied = await other.get(
          `/console/v1/merchant/orders/${trades.alipay}`,
        );
        expect(denied.status()).toBe(404);
        const deniedRefund = await other.post(
          `/console/v1/merchant/orders/${trades.alipay}/refund`,
          {
            headers: { "X-CSRF-Token": otherSession.data.csrf_token },
            data: { amount: "1.00", external_no: `${tag}-foreign` },
          },
        );
        expect(deniedRefund.status()).toBe(404);
        await mp.goto("/merchant/trade/order");
        const row = mp.getByRole("row").filter({ hasText: trades.alipay! });
        await expect(row).toBeVisible();
        await row
          .getByRole("button", {
            name: `订单操作 ${trades.alipay}`,
            exact: true,
          })
          .click();
        await mp.getByRole("menuitem", { name: "退款", exact: true }).click();
        // Eligibility is read before the actual funds dialog is opened.
        const dialog = mp.getByRole("dialog", {
          name: "申请退款",
          exact: true,
        });
        await expect(dialog).toBeVisible();
        await dialog.getByLabel("退款金额（元）", { exact: true }).fill("2.00");
        const external = `${tag}-partial`;
        await dialog.getByLabel("退款请求号", { exact: true }).fill(external);
        const response = mp.waitForResponse(
          (r) =>
            new URL(r.url()).pathname ===
              `/console/v1/merchant/orders/${trades.alipay}/refund` &&
            r.request().method() === "POST",
        );
        await dialog
          .getByRole("button", { name: "确认申请退款", exact: true })
          .click();
        const partial = await (await response).json();
        expect(partial.code, partial.message).toBe(0);
        expect(partial.data.status).toBe("succeeded");
        const replay = await m(`/orders/${trades.alipay}/refund`, "POST", {
          amount: "2.00",
          external_no: external,
        });
        expect(replay.id).toBe(partial.data.id);
        const conflicting = await merchant.request.post(
          `/console/v1/merchant/orders/${trades.alipay}/refund`,
          {
            headers: { "X-CSRF-Token": merchantCSRF },
            data: { amount: "1.00", external_no: external },
          },
        );
        expect(conflicting.status()).toBe(409);
        expect((await m("/dashboard")).available_money).toBe("27.40");
        const remaining = await m(`/orders/${trades.alipay}/refund`, "POST", {
          amount: "18.00",
          external_no: `${tag}-remaining`,
        });
        expect(remaining.status).toBe("succeeded");
        expect((await m("/dashboard")).available_money).toBe("9.80");
        const over = await merchant.request.post(
          `/console/v1/merchant/orders/${trades.alipay}/refund`,
          {
            headers: { "X-CSRF-Token": merchantCSRF },
            data: { amount: "0.01", external_no: `${tag}-over` },
          },
        );
        expect(over.status()).toBe(422);
        expect((await m("/dashboard")).available_money).toBe("9.80");
        checkpoints.push({
          stage: "refunded",
          available: "9.80",
          partial_refund: partial.data.id,
          final_refund: remaining.id,
        });
      });

      await test.step("merchant applies, admin rejects then approves and pays the original settlement once", async () => {
        await mp.goto("/merchant/settle/apply");
        const recipient = mp.getByRole("group", { name: "结算收款资料" });
        await expect(
          recipient.getByRole("button", { name: "配置收款资料", exact: true }),
        ).toBeEnabled();
        await recipient
          .getByRole("button", { name: "配置收款资料", exact: true })
          .click();
        await recipient
          .getByLabel("收款账号", { exact: true })
          .fill("local-lifecycle@example.invalid");
        await recipient
          .getByLabel("真实姓名", { exact: true })
          .fill("Local Lifecycle Recipient");
        await recipient
          .getByRole("button", { name: "保存收款版本", exact: true })
          .click();
        await expect(
          recipient.getByTestId("settlement-recipient-current"),
        ).toBeVisible();
        async function apply(amount: string, external: string) {
          await mp.getByLabel("金额（元）").fill(amount);
          await mp.getByLabel("业务请求号").fill(external);
          const response = mp.waitForResponse(
            (r) =>
              new URL(r.url()).pathname ===
                "/console/v1/merchant/workflows/settlements" &&
              r.request().method() === "POST",
          );
          await mp
            .getByRole("button", { name: "确认提交", exact: true })
            .click();
          await mp
            .getByRole("button", { name: "按已核对费用提交", exact: true })
            .click();
          const body = await (await response).json();
          expect(body.code, body.message).toBe(0);
          return body.data;
        }
        const rejected = await apply("1.00", `${tag}-reject`);
        expect((await m("/dashboard")).available_money).toBe("8.80");
        await ap.goto("/admin/settle/list");
        let row = ap.locator(`[data-settlement-id="${rejected.id}"]`);
        await row.getByRole("button", { name: "拒绝", exact: true }).click();
        await ap.getByRole("button", { name: "确认拒绝", exact: true }).click();
        await expect(row).toContainText("结算失败");
        expect((await m("/dashboard")).available_money).toBe("9.80");
        await mp.reload();
        const application = await apply("5.00", `${tag}-payout`);
        expect(String(application.settlement.fee_amount)).toBe("13");
        expect(String(application.settlement.net_amount)).toBe("487");
        expect((await m("/dashboard")).available_money).toBe("4.80");
        expect((await m("/dashboard")).frozen_money).toBe("5.00");
        await ap.reload();
        row = ap.locator(`[data-settlement-id="${application.id}"]`);
        await row.getByRole("button", { name: "核准", exact: true }).click();
        await ap.getByRole("button", { name: "确认核准", exact: true }).click();
        await row
          .getByRole("button", { name: "核对结算出款", exact: true })
          .click();
        const panel = ap.getByRole("region", { name: "结算出款", exact: true });
        await expect(panel).toContainText("允许代付 API");
        await expect(panel.getByLabel("出款通道")).toHaveCount(0);
        expect(
          (await a(`/settlement-payouts/${application.id}`)).payout,
        ).toBeNull();
        await ap.goto("/admin/merchant/merchant-list");
        const merchantRow = ap.getByRole("row").filter({ hasText: email });
        await merchantRow
          .getByRole("button", { name: "编辑", exact: true })
          .click();
        const editor = ap.getByRole("dialog");
        await editor.getByLabel("允许代付 API", { exact: true }).check();
        const changed = ap.waitForResponse(
          (r) =>
            new URL(r.url()).pathname ===
              `/console/v1/admin/resources/merchants/${merchantID}` &&
            r.request().method() === "PATCH",
        );
        await editor.getByRole("button", { name: "保存", exact: true }).click();
        const changeBody = await (await changed).json();
        expect(changeBody.code, changeBody.message).toBe(0);
        await ap.goto("/admin/settle/list");
        await ap
          .locator(`[data-settlement-id="${application.id}"]`)
          .getByRole("button", { name: "核对结算出款", exact: true })
          .click();
        await panel.getByLabel("出款通道").selectOption("1");
        await panel
          .getByRole("button", { name: "准备唯一出款绑定", exact: true })
          .click();
        await panel
          .getByRole("button", { name: "确认此操作", exact: true })
          .click();
        await panel
          .getByRole("button", { name: "发送原结算出款", exact: true })
          .click();
        const sendRequest = ap.waitForRequest(
          (request) =>
            new URL(request.url()).pathname ===
              `/console/v1/admin/settlement-payouts/${application.id}/execute` &&
            request.method() === "POST",
        );
        await panel
          .getByRole("button", { name: "确认发送并核对资金授权", exact: true })
          .click();
        await expect(panel).toContainText("已核实出款成功");
        await expect(
          panel.getByRole("button", { name: "发送原结算出款", exact: true }),
        ).toHaveCount(0);
        const paid = await a(`/settlement-payouts/${application.id}`);
        expect(paid.payout.state).toBe("succeeded");
        expect(paid.payout.transfer.amount).toBe("487");
        expect(paid.payout.funding.state).toBe("paid");
        const originalSend = await sendRequest;
        const replayedPayout = await a(
          `/settlement-payouts/${application.id}/execute`,
          "POST",
          originalSend.postDataJSON(),
        );
        expect(replayedPayout.transfer.id).toBe(paid.payout.transfer.id);
        expect(replayedPayout.state).toBe("succeeded");
        expect((await m("/dashboard")).available_money).toBe("4.80");
        expect((await m("/dashboard")).frozen_money).toBe("0.00");
        await mp.goto("/merchant/settle/list");
        await expect(
          mp.locator(`[data-settlement-id="${application.id}"]`),
        ).toContainText("已完成");
        await mp.reload();
        expect(
          (await a(`/settlement-payouts/${application.id}`)).payout.transfer.id,
        ).toBe(paid.payout.transfer.id);
        checkpoints.push({
          stage: "settled",
          available: "4.80",
          gross: "500",
          fee: "13",
          net: "487",
          settlement_id: application.id,
          transfer_id: paid.payout.transfer.id,
        });
      });
    });
    expect(errors).toEqual([]);
  } finally {
    await test.info().attach("lifecycle-checkpoints", {
      body: JSON.stringify(
        {
          synthetic_only: true,
          real_funds: false,
          checkpoints,
          browser_errors: errors,
        },
        null,
        2,
      ),
      contentType: "application/json",
    });
    for (const context of [admin, merchant])
      for (const page of context.pages())
        if (!page.isClosed())
          await page
            .screenshot({
              path: test
                .info()
                .outputPath(
                  `${context === admin ? "admin" : "merchant"}-final.png`,
                ),
              fullPage: true,
            })
            .catch(() =>
              test.info().annotations.push({
                type: "diagnostic",
                description: "Final screenshot unavailable after test failure",
              }),
            );
    await Promise.all([admin.close(), merchant.close(), other.dispose()]);
  }
});
