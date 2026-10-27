import { test, expect, type Page } from "@playwright/test";
import actual from "./parent-bindings180-http-fixture.json" with { type: "json" };
import buyerFee104 from "./fixtures/buyer-charge104-http.json" with { type: "json" };
const id = "9007199254740993",
  other = "9007199254740995";
async function fixture(
  page: Page,
  options: {
    operator?: boolean;
    defer?: boolean;
    conflict?: boolean;
    deferRead?: boolean;
    malformed?: boolean;
    wrongTarget?: boolean;
  } = {},
) {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  const reads: string[] = [];
  let release = () => {};
  let releaseRead = () => {},
    readFinished = false;
  const wait = new Promise<void>((r) => (release = r));
  const readWait = new Promise<void>((r) => (releaseRead = r));
  const original = {
    channel_id: id,
    pay_type: "alipay",
    methods: ["page", "scan"],
    configured: true,
    rate: "99.50",
    fee_rate_bps: 50,
    mode: 0,
    enabled: true,
    revision: "1",
    fingerprint: "a".repeat(64),
    can_update: true,
  };
  let state = { ...original };
  const parent = (channel: string) => ({
    ...actual.bound, id: channel, source_channel_id: channel,
    name: "原父通道 " + channel, pay_type: "alipay", plugin: "alipay",
    rate: channel === id ? state.rate : null,
    fee_rate_bps: channel === id ? state.fee_rate_bps : null,
    enabled: channel === id ? state.enabled : true,
    account: { plugin: "alipay", app_types: ["1"], mode: 0, fields: [] },
  });
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = { items: [], total: 0 },
      status = 200;
    if (req.method() !== "GET")
      writes.push({ path: p, body: req.postDataJSON() });
    else reads.push(p);
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: options.operator ? "operator" : "super",
        },
        csrf_token: "channel168-local",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/channels/schema"))
      data = {
        title: "通道资料",
        permissions: { create: true, update: true, delete: false },
        fields: [
          { name: "name", label: "名称", type: "string" },
          { name: "provider", label: "支付方式", type: "string" },
          { name: "enabled", label: "启用", type: "boolean" },
        ],
      };
    else if (p.endsWith("/resources/channels"))
      data = {
        items: [
          { id: "1", name: "原通道资料", provider: "alipay", enabled: true },
        ],
        total: 1,
      };
    else if (p.endsWith("/payment-channels/templates"))
      data = { items: [], total: 0 };
    else if (p.endsWith("/payment-channels"))
      data = { items: [parent(id), parent(other)], total: 2, page: 1, size: 20 };
    else if (p.includes("/buyer-fees/parents/") && req.method() === "GET")
      data = { ...buyerFee104.parent, channel_id: p.split("/").at(-1) };
    else if (p.endsWith("/channel-policies"))
      data = {
        items: [
          state,
          {
            ...state,
            channel_id: other,
            pay_type: "wxpay",
            methods: ["native"],
            configured: false,
            enabled: true,
            rate: null,
            fee_rate_bps: null,
            revision: "0",
            fingerprint: "c".repeat(64),
          },
        ],
        total: 2,
      };
    else if (p.endsWith("/channel-policies/" + id)) {
      if (req.method() === "PATCH") {
        expect(req.postDataJSON()).toEqual({
          rate: "98.75",
          mode: 0,
          enabled: false,
          expected_fingerprint: "a".repeat(64),
        });
        if (options.defer) await wait;
        if (options.conflict) {
          status = 409;
          data = null;
        } else
          state = {
            ...state,
            rate: "98.75",
            fee_rate_bps: 125,
            enabled: false,
            revision: "2",
            fingerprint: "b".repeat(64),
          };
        if (options.malformed && status === 200) {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: '{"code":0,"data":',
          });
          return;
        }
      }
      if (status === 200)
        data = options.wrongTarget
          ? { ...state, channel_id: other }
          : { ...state };
      if (options.deferRead && req.method() === "GET") {
        await readWait;
        readFinished = true;
      }
    } else if (p.endsWith("/channel-policies/" + other)) {
      data = {
        ...original,
        channel_id: other,
        pay_type: "wxpay",
        methods: ["native"],
        configured: false,
        rate: null,
        fee_rate_bps: null,
        revision: "0",
        fingerprint: "c".repeat(64),
      };
    }
    await route
      .fulfill({
        status,
        json: {
          code: status === 200 ? 0 : -1,
          message: status === 409 ? "changed" : "",
          data,
        },
      })
      .catch((e) => {
        if (!(options.deferRead && req.method() === "GET")) throw e;
      });
  });
  await page.goto("/admin/payapi/pay-channel");
  return {
    writes,
    reads,
    release,
    releaseRead,
    readFinished: () => readFinished,
    panel: page.getByRole("region", { name: "支付通道", exact: true }),
  };
}
async function openPolicy(page: Page, target = id) {
  const open = page.getByRole("button", { name: "通道 " + target + " 更多操作", exact: true });
  await open.click();
  await page.getByRole("menuitem", { name: "设置费率", exact: true }).click();
  return open;
}
for (const width of [1280, 375])
  test(
    "actual channel fee policy keeps exact target and explicit confirmation at " +
      width,
    async ({ page }) => {
      await page.setViewportSize({ width, height: 1000 });
      const f = await fixture(page);
      await expect(f.panel).toBeVisible();
      const row = f.panel
        .getByRole("row")
        .filter({ has: page.getByRole("cell", { name: id, exact: true }) });
      await expect(row).toContainText("99.50%");
      await expect(f.panel).toContainText("原费率（未配置）");
      const open = await openPolicy(page);
      const dialog = page.getByRole("dialog", {
        name: "设置支付通道费率",
        exact: true,
      });
      await expect(dialog).toContainText(id);
      const rate = dialog.getByLabel("支付费率（商户分成%）", { exact: true });
      await expect(rate).toBeFocused();
      await rate.fill("98.75");
      await dialog.getByLabel("启用收款", { exact: true }).uncheck();
      await dialog
        .getByRole("button", { name: "保存费率设置", exact: true })
        .click();
      const confirmation = dialog.getByRole("group", {
        name: "确认修改支付通道",
        exact: true,
      });
      await expect(confirmation).toContainText("98.75%");
      await expect(confirmation).toContainText("1.25%");
      await expect(confirmation).toContainText(id);
      expect(f.writes).toHaveLength(0);
      await confirmation
        .getByRole("button", { name: "返回修改", exact: true })
        .click();
      await expect(rate).toHaveValue("98.75");
      await dialog
        .getByRole("button", { name: "保存费率设置", exact: true })
        .click();
      await confirmation
        .getByRole("button", { name: "确定保存", exact: true })
        .click();
      await expect(
        dialog.getByRole("status").filter({ hasText: "费率设置已保存" }),
      ).toContainText("费率设置已保存");
      expect(f.writes).toHaveLength(1);
      expect(f.writes[0]!.path).toBe(
        "/console/v1/admin/channel-policies/" + id,
      );
      await dialog.screenshot({
        path: test.info().outputPath("channel-policy-" + width + ".png"),
      });
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
      await expect(open).toBeFocused();
      await expect(row).toContainText("98.75%");
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    },
  );
test("operator does not load or write super channel fee policies", async ({
  page,
}) => {
  const f = await fixture(page, { operator: true });
  await expect(f.panel).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
  expect(f.reads.filter((p) => p.includes("/channel-policies"))).toHaveLength(
    0,
  );
});

test("committed policy with malformed response keeps confirmation state unavailable until explicit refresh", async ({
  page,
}) => {
  const f = await fixture(page, { malformed: true });
  await openPolicy(page);
  const dialog = page.getByRole("dialog", {
    name: "设置支付通道费率",
    exact: true,
  });
  await dialog
    .getByLabel("支付费率（商户分成%）", { exact: true })
    .fill("98.75");
  await dialog.getByLabel("启用收款", { exact: true }).uncheck();
  await dialog
    .getByRole("button", { name: "保存费率设置", exact: true })
    .click();
  await dialog.getByRole("button", { name: "确定保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("修改未能确认");
  await expect(
    dialog.getByRole("button", { name: "刷新通道配置", exact: true }),
  ).toBeFocused();
  await expect(
    dialog.getByRole("button", { name: "确定保存", exact: true }),
  ).toHaveCount(0);
  expect(f.writes).toHaveLength(1);
  await dialog
    .getByRole("button", { name: "刷新通道配置", exact: true })
    .click();
  await expect(
    dialog.getByLabel("支付费率（商户分成%）", { exact: true }),
  ).toHaveValue("98.75");
  expect(f.writes).toHaveLength(1);
});

test("late detail for a closed channel cannot replace the next complete channel target", async ({
  page,
}) => {
  const f = await fixture(page, { deferRead: true });
  await openPolicy(page);
  let dialog = page.getByRole("dialog", {
    name: "设置支付通道费率",
    exact: true,
  });
  await expect
    .poll(
      () => f.reads.filter((p) => p.endsWith("/channel-policies/" + id)).length,
    )
    .toBe(1);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await openPolicy(page, other);
  dialog = page.getByRole("dialog", { name: "设置支付通道费率", exact: true });
  await expect(
    dialog.getByLabel("支付费率（商户分成%）", { exact: true }),
  ).toHaveValue("");
  f.releaseRead();
  await expect.poll(f.readFinished).toBe(true);
  await expect(dialog).toContainText(other);
  await expect(dialog).not.toContainText(id);
  await expect(
    dialog.getByLabel("支付费率（商户分成%）", { exact: true }),
  ).toHaveValue("");
  expect(f.writes).toHaveLength(0);
});

test("mismatched server target never enables an administrator write", async ({
  page,
}) => {
  const f = await fixture(page, { wrongTarget: true });
  await openPolicy(page);
  const dialog = page.getByRole("dialog", {
    name: "设置支付通道费率",
    exact: true,
  });
  await expect(dialog.getByRole("alert")).toContainText("无法核实");
  await expect(
    dialog.getByRole("button", { name: "保存费率设置", exact: true }),
  ).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});
test("pending channel policy write blocks duplicate submit and close", async ({
  page,
}) => {
  const f = await fixture(page, { defer: true });
  await openPolicy(page);
  const dialog = page.getByRole("dialog", {
    name: "设置支付通道费率",
    exact: true,
  });
  await dialog
    .getByLabel("支付费率（商户分成%）", { exact: true })
    .fill("98.75");
  await dialog.getByLabel("启用收款", { exact: true }).uncheck();
  await dialog
    .getByRole("button", { name: "保存费率设置", exact: true })
    .click();
  await dialog.getByRole("button", { name: "确定保存", exact: true }).click();
  await expect.poll(() => f.writes.length).toBe(1);
  await expect(
    dialog.getByRole("button", { name: "关闭", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Enter");
  expect(f.writes).toHaveLength(1);
  f.release();
  await expect(
    dialog.getByRole("status").filter({ hasText: "费率设置已保存" }),
  ).toContainText("费率设置已保存");
});
test("conflicted policy requires refresh and never silently retries a newer fingerprint", async ({
  page,
}) => {
  const f = await fixture(page, { conflict: true });
  await openPolicy(page);
  const dialog = page.getByRole("dialog", {
    name: "设置支付通道费率",
    exact: true,
  });
  await dialog
    .getByLabel("支付费率（商户分成%）", { exact: true })
    .fill("98.75");
  await dialog.getByLabel("启用收款", { exact: true }).uncheck();
  await dialog
    .getByRole("button", { name: "保存费率设置", exact: true })
    .click();
  await dialog.getByRole("button", { name: "确定保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("已变更");
  await expect(
    dialog.getByRole("button", { name: "刷新通道配置", exact: true }),
  ).toBeFocused();
  expect(f.writes).toHaveLength(1);
  await expect(
    dialog.getByRole("button", { name: "确定保存", exact: true }),
  ).toHaveCount(0);
});
