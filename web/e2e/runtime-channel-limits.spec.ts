import { test, expect, type Page } from "@playwright/test";
import actual from "./parent-bindings180-http-fixture.json" with { type: "json" };
const id = "9007199254740993",
  other = "9007199254740995";
async function fixture(
  page: Page,
  options: {
    operator?: boolean;
    deferWrite?: boolean;
    deferRead?: boolean;
    failure?: "conflict" | "malformed" | "abort" | "target";
  } = {},
) {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  let release = () => {},
    releaseRead = () => {};
  const wait = new Promise<void>((r) => (release = r)),
    readWait = new Promise<void>((r) => (releaseRead = r));
  const fields = {
    daytop: "100.00",
    daymaxorder: "2",
    paymin: "0.01",
    paymax: "100.00",
    timestart: "23",
    timestop: "2",
    rate_limit_seconds: "30",
    rate_limit_count: "3",
  };
  const original = {
    ...fields,
    channel_id: id,
    pay_type: "alipay",
    configured: true,
    revision: "1",
    fingerprint: "a".repeat(64),
    can_update: true,
    timezone: "UTC+08:00",
    semantics: "successful-threshold-v1",
    usage: {
      day: "2026-10-26",
      successful_amount: "150.00",
      successful_count: "1",
      recent_starts: "2",
      day_paused: true,
      rate_limited: false,
      as_of: "2026-10-26T06:00:00Z",
    },
  };
  let state = { ...original };
  const parent = (channel: string) => ({
    ...actual.bound, id: channel, source_channel_id: channel,
    name: "原父通道 " + channel, pay_type: "alipay", plugin: "alipay",
    account: { plugin: "alipay", app_types: ["1"], mode: 0, fields: [] },
  });
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      p = new URL(req.url()).pathname;
    let data: unknown = { items: [], total: 0 },
      status = 200;
    if (req.method() !== "GET")
      writes.push({ path: p, body: req.postDataJSON() });
    if (p.endsWith("/session"))
      data = {
        user: {
          id: "1",
          role: "admin",
          admin_class: options.operator ? "operator" : "super",
        },
        csrf_token: "limits-local",
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
        fields: [{ name: "name", label: "名称", type: "string" }],
      };
    else if (p.endsWith("/payment-channels/templates"))
      data = { items: [], total: 0 };
    else if (p.endsWith("/payment-channels"))
      data = { items: [parent(id), parent(other)], total: 2, page: 1, size: 20 };
    else if (/\/payment-channels\/\d+\/subject$/.test(p))
      data = { channel_id: p.split("/").at(-2), ordername: "", ordername_rules: "", fingerprint: "d".repeat(64) };
    else if (p.endsWith("/channel-policies"))
      data = {
        items: [id, other].map((channel_id) => ({
          channel_id,
          pay_type: "alipay",
          methods: ["native"],
          configured: false,
          rate: null,
          fee_rate_bps: null,
          mode: 0,
          enabled: true,
          revision: "0",
          fingerprint: "c".repeat(64),
          can_update: true,
        })),
        total: 2,
      };
    else if (p.endsWith("/limits")) {
      const target = p.split("/").at(-2)!;
      if (req.method() === "PATCH") {
        expect(req.headers()["x-csrf-token"]).toBe("limits-local");
        if (options.deferWrite) await wait;
        if (options.failure === "conflict") {
          status = 409;
          data = null;
        } else {
          const b = req.postDataJSON();
          state = {
            ...state,
            ...b,
            channel_id: target,
            revision: "2",
            fingerprint: "b".repeat(64),
          };
          delete (state as Record<string, unknown>).expected_fingerprint;
          const cents = (v: string) => BigInt(v.replace(".", ""));
          state.usage = {
            ...state.usage,
            day_paused:
              (cents(state.daytop) > 0n &&
                cents(state.usage.successful_amount) >= cents(state.daytop)) ||
              (BigInt(state.daymaxorder) > 0n &&
                BigInt(state.usage.successful_count) >=
                  BigInt(state.daymaxorder)),
            rate_limited:
              BigInt(state.rate_limit_seconds) > 0n &&
              BigInt(state.rate_limit_count) > 0n &&
              BigInt(state.usage.recent_starts) >=
                BigInt(state.rate_limit_count),
          };
          data = state;
        }
        if (options.failure === "abort") {
          await route.abort("failed");
          return;
        }
        if (options.failure === "malformed") {
          await route.fulfill({
            status: 200,
            contentType: "application/json",
            body: "{",
          });
          return;
        }
      } else {
        if (options.deferRead && target === id) await readWait;
        data = {
          ...state,
          channel_id: options.failure === "target" ? other : target,
        };
      }
    }
    await route
      .fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify({
          code: status === 200 ? 0 : -1,
          message: "",
          data,
        }),
      })
      .catch((e) => {
        if (!options.deferRead) throw e;
      });
  });
  await page.goto("/admin/payapi/pay-channel");
  const panel = page.getByRole("region", { name: "支付通道", exact: true });
  const launch = (target = id) =>
    panel
      .getByRole("row")
      .filter({ has: page.getByRole("cell", { name: target, exact: true }) })
      .getByRole("button", { name: "通道 " + target + " 更多操作", exact: true });
  const open = async (target = id) => {
    await launch(target).click();
    await page.getByRole("menuitem", { name: "高级设置", exact: true }).click();
  };
  return { writes, release, releaseRead, panel, launch, open, fields };
}
const dialog = (page: Page) =>
  page.getByRole("dialog", { name: "父通道限额设置", exact: true });
async function confirm(page: Page) {
  await dialog(page)
    .getByLabel("单日限额（成功实收元）", { exact: true })
    .fill("200.00");
  await dialog(page)
    .getByRole("button", { name: "保存限额设置", exact: true })
    .click();
}
for (const width of [1440, 375])
  test(
    "global parent original fields and real usage at " + width,
    async ({ page }) => {
      await page.setViewportSize({ width, height: 1000 });
      const f = await fixture(page);
      await f.open();
      const d = dialog(page);
      await expect(
        d.getByLabel("单日限额（成功实收元）", { exact: true }),
      ).toBeFocused();
      await expect(d).toContainText(id);
      await page.keyboard.press("Shift+Tab");
      await expect(
        d.getByRole("button", { name: "关闭", exact: true }),
      ).toBeFocused();
      await page.keyboard.press("Tab");
      await expect(
        d.getByLabel("单日限额（成功实收元）", { exact: true }),
      ).toBeFocused();
      await expect(d).toContainText("150.00 元");
      await expect(d).toContainText("UTC+8");
      await expect(d).toContainText("原请求money");
      await expect(d).toContainText("已暂停新单");
      await d.evaluate((el) => {
        el.scrollTop = 0;
      });
      await page.screenshot({
        path: test.info().outputPath("limits-" + width + ".png"),
        fullPage: true,
      });
      expect(await d.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(
        true,
      );
      await confirm(page);
      expect(
        await page.evaluate(
          () =>
            document.documentElement.scrollWidth <=
            document.documentElement.clientWidth,
        ),
      ).toBe(true);
      expect(f.writes).toHaveLength(0);
      await expect(
        d.getByRole("button", { name: "返回修改", exact: true }),
      ).toBeFocused();
      await d.getByRole("button", { name: "确定保存", exact: true }).click();
      await expect(d.getByRole("status").filter({ hasText: "限额设置已保存" })).toContainText("限额设置已保存");
      expect(f.writes).toHaveLength(1);
      expect(f.writes[0]).toEqual({
        path: "/console/v1/admin/channel-policies/" + id + "/limits",
        body: {
          ...f.fields,
          daytop: "200.00",
          expected_fingerprint: "a".repeat(64),
        },
      });
      await d.getByRole("button", { name: "关闭", exact: true }).click();
      await expect(f.launch()).toBeFocused();
    },
  );
test("original operator cannot see global parent limits", async ({ page }) => {
  const f = await fixture(page, { operator: true });
  await expect(f.panel).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});
for (const failure of ["conflict", "malformed", "abort"] as const)
  test(
    "uncertain global limits save must refresh without blind retry: " + failure,
    async ({ page }) => {
      const f = await fixture(page, { failure });
      await f.open();
      await confirm(page);
      await dialog(page)
        .getByRole("button", { name: "确定保存", exact: true })
        .click();
      await expect(dialog(page).getByRole("alert")).toContainText(
        failure === "conflict" ? "已变更" : "未能确认",
      );
      await expect(
        dialog(page).getByRole("button", { name: "刷新限额状态", exact: true }),
      ).toBeFocused();
      await expect(
        dialog(page).getByRole("button", { name: "确定保存", exact: true }),
      ).toHaveCount(0);
      expect(f.writes).toHaveLength(1);
    },
  );
test("pending global limits write locks duplicate submit, escape and background parent switches", async ({
  page,
}) => {
  const f = await fixture(page, { deferWrite: true });
  await f.open();
  await confirm(page);
  await dialog(page)
    .getByRole("button", { name: "确定保存", exact: true })
    .click();
  await expect.poll(() => f.writes.length).toBe(1);
  await expect(
    dialog(page).getByRole("button", { name: "关闭", exact: true }),
  ).toBeDisabled();
  await expect(f.launch(other)).toBeDisabled();
  await page.keyboard.press("Escape");
  await page.keyboard.press("Enter");
  await expect(dialog(page)).toBeVisible();
  expect(f.writes).toHaveLength(1);
  f.release();
  await expect(dialog(page).getByRole("status").filter({ hasText: "限额设置已保存" })).toContainText("已保存");
});
test("late limit read cannot replace another exact parent target", async ({
  page,
}) => {
  const f = await fixture(page, { deferRead: true });
  await f.open();
  await dialog(page).getByRole("button", { name: "关闭", exact: true }).click();
  await f.open(other);
  await expect(dialog(page)).toContainText(other);
  f.releaseRead();
  await expect(dialog(page)).toContainText(other);
  await expect(dialog(page)).not.toContainText(id);
  expect(f.writes).toHaveLength(0);
});
test("wrong target state never enables a parent limits write", async ({
  page,
}) => {
  const f = await fixture(page, { failure: "target" });
  await f.open();
  await expect(dialog(page).getByRole("alert")).toContainText("无法核实");
  await expect(
    dialog(page).getByRole("button", { name: "保存限额设置", exact: true }),
  ).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});

test("a conflicted global draft survives explicit refresh but never submits automatically", async ({
  page,
}) => {
  const f = await fixture(page, { failure: "conflict" });
  await f.open();
  await confirm(page);
  await dialog(page)
    .getByRole("button", { name: "确定保存", exact: true })
    .click();
  await expect(dialog(page).getByRole("alert")).toContainText("已变更");
  await dialog(page)
    .getByRole("button", { name: "刷新限额状态", exact: true })
    .click();
  await expect(
    dialog(page).getByLabel("单日限额（成功实收元）", { exact: true }),
  ).toHaveValue("200.00");
  await expect(dialog(page).getByRole("status").filter({ hasText: "草稿已保留" })).toContainText("草稿已保留");
  expect(f.writes).toHaveLength(1);
});
test("invalid single amount intersection cannot open a save confirmation", async ({
  page,
}) => {
  const f = await fixture(page);
  await f.open();
  await dialog(page)
    .getByLabel("单笔最小（原请求money元）", { exact: true })
    .fill("101.00");
  await dialog(page)
    .getByRole("button", { name: "保存限额设置", exact: true })
    .click();
  await expect(dialog(page).getByRole("alert")).toContainText("最小值不能大于");
  await expect(
    dialog(page).getByRole("button", { name: "确定保存", exact: true }),
  ).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});
