import { pageReload248 } from "./fixtures/page-reload248";
import { test, expect, type Page } from "@playwright/test";
import { accountSettingsReadFixture } from "./fixtures/account-settings";
import actualPlatform from "./globalaccess179-http-fixture.json" with { type: "json" };
const mid = "9007199254740993",
  csrf = "c".repeat(43);
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
async function fixture(page: Page, mode = "normal") {
  const writes: {
      path: string;
      body: Record<string, unknown>;
      csrf: string | undefined;
    }[] = [],
    errors: string[] = [];
  let state = {
    merchant_id: mid,
    keylogin: 1,
    refund: 1,
    transfer: 0,
    configured: false,
    revision: "0",
    fingerprint: "a".repeat(64),
    can_update: true,
    transfer_granted: false,
  };
  page.on("pageerror", (e) => errors.push(e.message));
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.route("**/console/v1/**", async (r) => {
    const q = r.request(),
      path = new URL(q.url()).pathname;
    if (path === "/console/v1/public/merchant-access")
      return r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          code: 0,
          message: "",
          data: actualPlatform.public_default,
        }),
      });
    let data: unknown = accountSettingsReadFixture(path, mid) ?? {
      items: [],
      total: 0,
    };
    if (q.method() !== "GET")
      writes.push({
        path,
        body: q.postDataJSON(),
        csrf: q.headers()["x-csrf-token"],
      });
    if (path.endsWith("/session"))
      data = {
        user: { id: mid, name: "本地商户", role: "merchant" },
        csrf_token: csrf,
        ...(mode === "delegated"
          ? { delegated_by: { id: "1", name: "super" } }
          : {}),
      };
    else if (path.endsWith("/account/api-switches")) {
      if (q.method() === "PATCH") {
        const b = q.postDataJSON();
        expect(b).toEqual({
          keylogin: 0,
          refund: 0,
          transfer: 1,
          expected_fingerprint: "a".repeat(64),
        });
        expect(q.headers()["x-csrf-token"]).toBe(csrf);
        if (mode === "conflict")
          return r.fulfill({
            status: 409,
            contentType: "application/json",
            body: JSON.stringify({ code: -1, message: "changed" }),
          });
        state = {
          ...state,
          configured: true,
          revision: "1",
          keylogin: 0,
          refund: 0,
          transfer: 1,
          fingerprint: "b".repeat(64),
        };
        if (mode === "lost") return r.abort("failed");
        if (mode === "malformed")
          return r.fulfill({
            status: 200,
            contentType: "application/json",
            body: "{",
          });
      }
      data = state;
    } else if (path.endsWith("/account/profile"))
      data = { name: "本地商户", email: "local@example.invalid", pay: "1" };
    else if (path.endsWith("/security/totp")) data = { enabled: false };
    return r.fulfill(ok(data));
  });
  await page.goto("/merchant/account/settings");
  const panel = page.getByRole("region", {
    name: "接口与登录功能设置",
    exact: true,
  });
  return { panel, writes, errors };
}
async function change(panel: ReturnType<Page["getByRole"]>) {
  await panel.getByRole("checkbox", { name: "开启密钥登录" }).uncheck();
  await panel.getByRole("checkbox", { name: "订单退款API接口" }).uncheck();
  await panel.getByRole("checkbox", { name: "代付API接口" }).check();
}
for (const width of [1440, 375])
  test(`original API switches and independent 172 form at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await expect(
      f.panel.getByRole("checkbox", { name: "开启密钥登录" }),
    ).toBeChecked();
    await expect(f.panel).toContainText("仍需管理员");
    await change(f.panel);
    await f.panel
      .getByRole("button", { name: "保存功能开关", exact: true })
      .click();
    expect(f.writes).toHaveLength(0);
    await f.panel.getByRole("button", { name: "返回修改" }).click();
    expect(f.writes).toHaveLength(0);
    await f.panel
      .getByRole("button", { name: "保存功能开关", exact: true })
      .click();
    await f.panel
      .getByRole("button", { name: "确认保存", exact: true })
      .click();
    await expect(f.panel.getByRole("status")).toHaveText("保存成功");
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0].path).toBe("/console/v1/merchant/account/api-switches");
    await expect(
      page
        .getByRole("region", { name: "个人功能设置", exact: true })
        .getByLabel("联系QQ"),
    ).toHaveValue("");
    expect(f.errors).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await f.panel.evaluate((el) => {
      const bar = document.querySelector(".topbar")?.getBoundingClientRect();
      window.scrollTo({
        top:
          window.scrollY +
          el.getBoundingClientRect().top -
          (bar?.bottom ?? 64) -
          16,
        behavior: "instant",
      });
    });
    await expect
      .poll(async () =>
        f.panel
          .getByRole("heading", { name: "接口与登录功能设置", exact: true })
          .evaluate((el) => {
            const rect = el.getBoundingClientRect(),
              bar = document.querySelector(".topbar")!.getBoundingClientRect();
            return (
              rect.top >= bar.bottom &&
              rect.left >= 0 &&
              rect.right <= innerWidth
            );
          }),
      )
      .toBe(true);
    await f.panel.screenshot({
      path: info.outputPath(`merchant174-switches-${width}.png`),
    });
  });
for (const mode of ["conflict", "lost", "malformed"])
  test(`${mode} switch save requires explicit reread and does not repeat PATCH`, async ({
    page,
  }) => {
    const f = await fixture(page, mode);
    await change(f.panel);
    await f.panel
      .getByRole("button", { name: "保存功能开关", exact: true })
      .click();
    await f.panel
      .getByRole("button", { name: "确认保存", exact: true })
      .click();
    await expect(f.panel.getByRole("alert")).toContainText("刷新页面");
    await expect(
      f.panel.getByRole("button", { name: "保存功能开关", exact: true }),
    ).toBeDisabled();
    expect(f.writes).toHaveLength(1);
    await pageReload248(page);
    await expect(
      f.panel.getByRole("checkbox", { name: "开启密钥登录" }),
    ).toBeVisible();
    expect(f.writes).toHaveLength(1);
    expect(f.errors).toEqual([]);
  });
test("delegation cannot read or edit original private switches", async ({
  page,
}) => {
  const requests: string[] = [];
  page.on("request", (q) => {
    if (q.url().includes("/account/api-switches")) requests.push(q.url());
  });
  const f = await fixture(page, "delegated");
  await expect(f.panel).toContainText("商户本人登录");
  expect(requests).toEqual([]);
  expect(f.writes).toEqual([]);
});
