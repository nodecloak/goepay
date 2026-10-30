import { test, expect, type Page } from "@playwright/test";
const ok = (data: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify({ code: 0, data }),
});
const mid = "9007199254740993";
async function selectEditor(page: Page) {
  await page.locator(".rich-input").evaluate((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const s = window.getSelection();
    s?.removeAllRanges();
    s?.addRange(range);
  });
}
async function fixture(
  page: Page,
  area: "admin" | "merchant" = "admin",
  options: { guest?: boolean; unknown?: boolean; delegated?: boolean } = {},
) {
  await page.emulateMedia({ reducedMotion: "reduce" });
  const writes: {
      method: string;
      path: string;
      body: Record<string, unknown> | undefined;
    }[] = [],
    external: string[] = [],
    errors: string[] = [];
  let notices = [
    {
      id: "9007199254740995",
      content: "旧标题下的纯文本 <b>不执行</b>",
      title: "旧标题",
      sort: 50,
      color: "",
      content_format: "text",
      audience: "all",
      enabled: true,
      created_at: "2026-10-26T00:00:00Z",
    },
  ];
  const layout = {
    zhuce: "<p><strong>注册公告173</strong>：请核对注册信息</p>",
    footer: '<p>本地平台173 · <a href="/help">联系我们</a></p>',
    format: "html",
  };
  const settings = [
    { id: "11", key: "modal", value: "<p>欢迎设置173</p>" },
    { id: "12", key: "zhuce", value: layout.zhuce },
    { id: "13", key: "footer", value: layout.footer },
  ];
  const state = { date: "2026-10-26", delegated: options.delegated ?? false };
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("https://tracker.invalid/**", async (r) => {
    external.push(r.request().url());
    await r.abort();
  });
  await page.route("**/console/v1/**", async (r) => {
    const q = r.request(),
      u = new URL(q.url()),
      path = u.pathname,
      method = q.method();
    if (method !== "GET")
      writes.push({
        method,
        path,
        body: method === "DELETE" ? undefined : q.postDataJSON(),
      });
    if (path === "/console/v1/public/layout") return r.fulfill(ok(layout));
    if (path.endsWith("/session")) {
      if (options.guest)
        return r.fulfill({
          status: 401,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "请登录" }),
        });
      return r.fulfill(
        ok({
          user: {
            id: area === "merchant" ? mid : "1",
            name: "本地测试",
            role: area,
            admin_class: area === "admin" ? "super" : undefined,
          },
          csrf_token: "a".repeat(43),
          ...(state.delegated
            ? { delegated_by: { id: "2", name: "本地代登录" } }
            : {}),
        }),
      );
    }
    if (path.endsWith("/capabilities"))
      return r.fulfill(
        ok({
          registration_open: true,
          email_enabled: true,
          invite_required: false,
          expires_seconds: 600,
        }),
      );
    if (path.endsWith("/resources/announcements/schema"))
      return r.fulfill(
        ok({
          title: "公告管理",
          fields: [],
          permissions: { create: true, update: true, delete: true },
        }),
      );
    if (path.endsWith("/resources/announcements")) {
      if (method === "POST") {
        if (options.unknown) return r.abort("connectionreset");
        const body = q.postDataJSON();
        const row = {
          id: "9007199254740997",
          ...body,
          created_at: "2026-10-26T01:00:00Z",
        };
        notices = [row, ...notices];
        return r.fulfill(ok(row));
      }
      return r.fulfill(ok({ items: notices, total: notices.length }));
    }
    if (path.includes("/resources/announcements/")) {
      const id = path.split("/").at(-1)!,
        index = notices.findIndex((x) => x.id === id);
      if (index < 0) throw Error("unexpected announcement target");
      if (method === "DELETE") {
        notices.splice(index, 1);
        return r.fulfill(ok({}));
      }
      if (method === "PATCH")
        notices[index] = { ...notices[index], ...q.postDataJSON() };
      return r.fulfill(ok(notices[index]));
    }
    if (path.endsWith("/dashboard"))
      return r.fulfill(
        ok({
          orders: 0,
          paid_orders: 0,
          available_money: "0.00",
          frozen_money: "0.00",
          summary: {},
          trend: [],
          payment_mix: [],
          announcements: [
            {
              id: "6",
              content:
                '<p style="color:#123456"><strong>富文本公告173</strong></p>',
              content_format: "html",
              color: "#123456",
              created_at: "2026-10-26T00:00:00Z",
            },
          ],
          recent_settlements: [],
          merchant: { id: mid, name: "本地商户", pay: "1", enabled: true },
          business_date: state.date,
          welcome_notice: "<p>每日欢迎173</p>",
          fee_schedule: {},
          payment_channels: [],
        }),
      );
    if (path.endsWith("/resources/settings/schema"))
      return r.fulfill(
        ok({
          title: "系统设置",
          fields: [
            {
              name: "key",
              type: "select",
              options: ["modal", "zhuce", "footer"],
            },
          ],
          permissions: { create: true, update: true, delete: false },
        }),
      );
    if (path.endsWith("/resources/settings"))
      return r.fulfill(ok({ items: settings, total: 3 }));
    if (path.includes("/resources/settings/")) {
      const found = settings.find((x) => x.id === path.split("/").at(-1));
      if (!found) throw Error("wrong setting ID");
      if (method !== "GET") found.value = q.postDataJSON().value;
      return r.fulfill(ok(found));
    }
    if (path.endsWith("/security/payment-password"))
      return r.fulfill(
        ok({
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        }),
      );
    return r.fulfill(ok({ items: [], total: 0 }));
  });
  return { writes, external, errors, state };
}
for (const width of [1440, 375])
  test(`original announcement editor, conditional actions and safe HTML at ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const f = await fixture(page);
    await page.goto("/admin/system/anounce");
    await expect(
      page.getByRole("heading", { name: "添加公告", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("region", { name: "已发公告", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".announcement-list")).toContainText(
      "<b>不执行</b>",
    );
    expect(await page.locator(".announcement-list b").count()).toBe(0);
    await page.getByRole("button", { name: "编辑 HTML", exact: true }).click();
    const source = page.getByRole("dialog", { name: "编辑 HTML", exact: true });
    await source
      .getByRole("textbox", { name: "HTML 内容" })
      .fill(
        '<p style="color:#123456" onclick="window.__pwned173=1"><strong>公告173</strong></p><img src="https://tracker.invalid/pixel.png"><iframe src="https://tracker.invalid/frame"></iframe><script>window.__pwned173=1</script><table><tbody><tr><td>表格173</td></tr></tbody></table>',
      );
    await source.getByRole("button", { name: "应用", exact: true }).click();
    await page.getByLabel("排序", { exact: true }).fill("20");
    await page.getByLabel("公告文字颜色", { exact: true }).fill("#123456");
    await expect(page.locator(".rich-input strong")).toHaveText("公告173");
    await expect(page.locator(".rich-input img")).toHaveCount(0);
    await page.getByRole("button", { name: "添加", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("添加公告成功");
    expect(f.writes).toHaveLength(1);
    expect(f.writes[0].body).toMatchObject({
      sort: 20,
      color: "#123456",
      content_format: "html",
      audience: "merchant",
    });
    expect(JSON.stringify(f.writes)).not.toMatch(
      /tracker|onclick|script|iframe/,
    );
    const first = page.locator(".announcement-row").first();
    await expect(first.locator("strong")).toHaveText("公告173");
    await first.getByRole("button", { name: "隐藏", exact: true }).click();
    await expect(
      first.getByRole("button", { name: "显示", exact: true }),
    ).toBeVisible();
    await first.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "编辑公告", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "取消编辑", exact: true }).click();
    await first.getByRole("button", { name: "删除", exact: true }).click();
    await page
      .getByRole("dialog", { name: "删除公告", exact: true })
      .getByRole("button", { name: "取消", exact: true })
      .click();
    expect(f.writes.filter((x) => x.method === "DELETE")).toHaveLength(0);
    await first.getByRole("button", { name: "删除", exact: true }).click();
    await page
      .getByRole("dialog", { name: "删除公告", exact: true })
      .getByRole("button", { name: "确定删除", exact: true })
      .click();
    expect(f.writes.filter((x) => x.method === "DELETE")).toHaveLength(1);
    expect(f.external).toEqual([]);
    expect(await page.evaluate(() => Object.hasOwn(window, "__pwned173"))).toBe(
      false,
    );
    expect(f.errors).toEqual([]);
    await page.evaluate(() =>
      window.scrollTo({ top: 0, left: 0, behavior: "instant" }),
    );
    await expect
      .poll(async () =>
        page.locator(".announcement-editor h2").evaluate((el) => {
          const r = el.getBoundingClientRect(),
            bar = document.querySelector(".topbar")!.getBoundingClientRect(),
            main = document
              .querySelector(".main-content")!
              .getBoundingClientRect();
          return r.left >= main.left && r.top >= bar.bottom;
        }),
      )
      .toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`announcements-${width}.png`),
      fullPage: true,
    });
    const geometry = await page.evaluate(() => ({
      scrollX,
      scrollY,
      innerWidth,
      root: document.documentElement.scrollWidth,
      body: document.body.scrollWidth,
      rects: [
        ".workspace",
        ".main-content",
        ".console-sidebar",
        ".content",
        ".announcement-editor",
        ".announcement-editor h2",
      ].map((q) => {
        const el = document.querySelector(q)!;
        const r = el.getBoundingClientRect();
        return {
          q,
          x: r.x,
          y: r.y,
          width: r.width,
          margin: getComputedStyle(el).marginLeft,
          scrollLeft: el.scrollLeft,
        };
      }),
    }));
    if (width === 1440) {
      expect(geometry.rects.find((r) => r.q === ".main-content")!.x).toBe(230);
      expect(
        geometry.rects.find((r) => r.q === ".announcement-editor h2")!.x,
      ).toBeGreaterThan(230);
    }
    await info.attach("announcement-geometry", {
      body: JSON.stringify(geometry, null, 2),
      contentType: "application/json",
    });
  });
test("real editor formats, clears styles and sanitizes clipboard before insertion", async ({
  page,
}) => {
  const f = await fixture(page);
  await page.goto("/admin/system/anounce");
  const editor = page.getByRole("textbox", { name: "公告内容", exact: true });
  await editor.fill("排版173");
  await selectEditor(page);
  await page.getByRole("button", { name: "粗体", exact: true }).click();
  await expect(page.locator(".rich-input strong")).toHaveText("排版173");
  await selectEditor(page);
  await page.getByRole("button", { name: "清除样式", exact: true }).click();
  await expect(page.locator(".rich-input strong,.rich-input b")).toHaveCount(0);
  await selectEditor(page);
  await editor.evaluate((el) => {
    const data = new DataTransfer();
    data.setData(
      "text/html",
      '<p>粘贴173<img src="https://tracker.invalid/paste.png" onerror="window.__pwned173=1"></p>',
    );
    el.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData: data,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect(editor).toContainText("粘贴173");
  await expect(editor.locator("img")).toHaveCount(0);
  expect(f.external).toEqual([]);
  expect(f.errors).toEqual([]);
});
test("a failed create is not blindly repeated and needs explicit refresh", async ({
  page,
}) => {
  const f = await fixture(page, "admin", { unknown: true });
  await page.goto("/admin/system/anounce");
  const input = page.getByRole("textbox", { name: "公告内容", exact: true });
  await input.fill("未知结果173");
  await page.getByRole("button", { name: "添加", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("刷新核对");
  await expect(
    page.getByRole("button", { name: "添加", exact: true }),
  ).toBeDisabled();
  expect(f.writes).toHaveLength(1);
  await expect(input).toHaveText("未知结果173");
  await page.getByRole("button", { name: "刷新核对公告", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "添加", exact: true }),
  ).toBeEnabled();
  expect(f.writes).toHaveLength(1);
});
test("welcome uses own full identity and server day, once per day across reloads", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 375, height: 900 });
  const f = await fixture(page, "merchant");
  await page.goto("/merchant/dashboard");
  let dialog = page.getByRole("dialog", { name: "欢迎回来", exact: true });
  await expect(dialog).toContainText("每日欢迎173");
  await page.screenshot({
    path: info.outputPath("welcome-375.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "我知道了", exact: true }).click();
  await page.reload();
  await expect(page.getByText("富文本公告173", { exact: true })).toBeVisible();
  await expect(dialog).toHaveCount(0);
  f.state.date = "2026-10-27";
  await page.reload();
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  f.state.date = "2026-10-28";
  f.state.delegated = true;
  await page.reload();
  await expect(page.getByText("富文本公告173", { exact: true })).toBeVisible();
  await expect(dialog).toHaveCount(0);
  expect(
    await page.evaluate(
      (id) => localStorage.getItem("goepay:welcome:v1:" + id),
      mid,
    ),
  ).toBe("2026-10-27");
  expect(f.writes).toEqual([]);
  expect(f.errors).toEqual([]);
});
test("registration notice and footer render through the real public layout position", async ({
  page,
}) => {
  await fixture(page, "merchant", { guest: true });
  await page.goto("/merchant/dashboard");
  await expect(
    page.getByRole("heading", { name: "商户登录", exact: true }),
  ).toBeVisible();
  await expect(page.locator("footer")).toContainText("本地平台173");
  await page.getByRole("button", { name: "注册商户", exact: true }).click();
  await expect(page.locator(".registration-announcement strong")).toHaveText(
    "注册公告173",
  );
  await page.getByRole("button", { name: "返回登录", exact: true }).click();
  await expect(page.locator(".registration-announcement")).toHaveCount(0);
});
test("original layout fields edit rich content and blank explicitly disables welcome", async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const f = await fixture(page);
  await page.goto("/admin/system/settings");
  await page.getByRole("tab", { name: "公告排版", exact: true }).click();
  const box = page
    .locator(".setting-field")
    .filter({ has: page.getByText("用户中心弹出公告", { exact: true }) });
  await expect(
    box.getByRole("textbox", { name: "用户中心弹出公告", exact: true }),
  ).toContainText("欢迎设置173");
  await box.getByRole("button", { name: "编辑 HTML", exact: true }).click();
  const source = page.getByRole("dialog", { name: "编辑 HTML", exact: true });
  await source.getByRole("textbox", { name: "HTML 内容" }).fill("");
  await source.getByRole("button", { name: "应用", exact: true }).click();
  await page.getByRole("button", { name: "保存本页设置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("保存");
  expect(f.writes).toHaveLength(1);
  expect(f.writes[0]).toMatchObject({
    method: "PUT",
    path: "/console/v1/admin/resources/settings/11",
    body: { value: "" },
  });
  await page.screenshot({
    path: info.outputPath("announcement-layout-1440.png"),
    fullPage: true,
  });
  expect(f.errors).toEqual([]);
});
