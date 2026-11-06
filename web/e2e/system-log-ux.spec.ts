import { test, expect } from "@playwright/test";
import { chooseKitOption } from "./kit-select-helpers";
const big = "9007199254740993";
const rows = [
  {
    source: "login",
    id: big,
    uid: "1",
    subject_kind: "merchant",
    type: "登录成功",
    ip: "192.0.2.1",
    date: "2026-10-25T00:01:00Z",
  },
  {
    source: "business",
    id: big,
    uid: "0",
    subject_kind: "admin",
    type: "修改 · 用户组设置",
    ip: "",
    date: "2026-10-25T00:02:00Z",
  },
  {
    source: "login",
    id: "9007199254740995",
    uid: null,
    subject_kind: "unknown",
    type: "登录失败",
    ip: "192.0.2.1",
    date: "2026-10-25T00:03:00Z",
  },
];
for (const width of [1280, 375])
  test(`original system log columns and explicit UID/IP scope remain read only at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const reads: URL[] = [],
      writes: string[] = [];
    await page.route("**/console/v1/**", (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        p = u.pathname;
      if (req.method() !== "GET") writes.push(p);
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "system134",
        };
      else if (p.endsWith("/system-log")) {
        reads.push(u);
        const value = u.searchParams.get("value"),
          selected =
            value === big
              ? []
              : u.searchParams.get("column") === "ip"
                ? rows.filter((r) => r.ip === value)
                : rows;
        data = { items: selected, total: selected.length };
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ code: 0, data }),
      });
    });
    await page.goto("/admin/system/log");
    await expect.poll(() => reads.length).toBe(1);
    await expect(page.getByRole("columnheader")).toHaveText([
      "ID",
      "商户号",
      "操作类型",
      "操作IP",
      "时间",
    ]);
    await expect(page.locator(".id-cell").filter({ hasText: big })).toHaveCount(
      2,
    );
    await expect(
      page.getByRole("link", { name: "1", exact: true }),
    ).toHaveAttribute("href", "/admin/merchant/merchant-list?uid=1");
    await expect(
      page.getByRole("cell", { name: "管理员", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "未核实", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("2026-10-25 08:01:00", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "192.0.2.1", exact: true }).first(),
    ).toHaveAttribute("href", "https://m.ip138.com/iplookup.asp?ip=192.0.2.1");
    await chooseKitOption(
      page,
      page.getByRole("button", { name: "搜索字段", exact: true }),
      "操作IP",
    );
    await page.getByLabel("搜索内容", { exact: true }).fill("192.0.2.1");
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect
      .poll(() => reads.at(-1)?.searchParams.get("column"))
      .toBe("ip");
    expect(reads.at(-1)?.searchParams.get("value")).toBe("192.0.2.1");
    await page.goto("/admin/system/log?uid=" + big);
    await expect(page.getByLabel("搜索内容", { exact: true })).toHaveValue(big);
    await expect.poll(() => reads.at(-1)?.searchParams.get("value")).toBe(big);
    expect(writes).toHaveLength(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
  });

test("late subject logs cannot replace the next UID and failed refresh clears previous rows", async ({
  page,
}) => {
  let release: (() => void) | undefined,
    started = false,
    fail = false;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/console/v1/**", async (route) => {
    const u = new URL(route.request().url()),
      p = u.pathname;
    let data: unknown = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", role: "admin", admin_class: "super" },
        csrf_token: "system-late134",
      };
    else if (p.endsWith("/system-log")) {
      const uid = u.searchParams.get("value");
      if (uid === "1") {
        started = true;
        await gate;
      }
      if (fail)
        return route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ code: -1, message: "本地日志读取失败" }),
        });
      data = { items: [{ ...rows[0], uid }], total: 1 };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/system/log?uid=1");
  await expect.poll(() => started).toBe(true);
  await page.getByLabel("搜索内容", { exact: true }).fill("2");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "2", exact: true }),
  ).toBeVisible();
  release!();
  await expect(page.getByRole("link", { name: "1", exact: true })).toHaveCount(
    0,
  );
  fail = true;
  await page.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("本地日志读取失败");
  await expect(page.getByRole("link", { name: "2", exact: true })).toHaveCount(
    0,
  );
});

test("ordinary administrator keeps original log access and ambiguous links make no query until reset", async ({
  page,
}) => {
  const reads: URL[] = [];
  await page.route("**/console/v1/**", (route) => {
    const u = new URL(route.request().url()),
      p = u.pathname;
    let data: unknown = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "2", role: "admin", admin_class: "operator" },
        csrf_token: "system-operator134",
      };
    else if (p.endsWith("/system-log")) {
      reads.push(u);
      data = { items: [rows[1]], total: 1 };
    }
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, data }),
    });
  });
  await page.goto("/admin/system/log?uid=1&column=ip&value=192.0.2.1");
  await expect(page.getByRole("alert")).toContainText("日志链接");
  expect(reads).toHaveLength(0);
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await expect.poll(() => reads.length).toBe(1);
  await page.getByLabel("搜索内容", { exact: true }).fill("0");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect.poll(() => reads.at(-1)?.searchParams.get("value")).toBe("0");
  await expect(
    page.getByRole("cell", { name: "管理员", exact: true }),
  ).toBeVisible();
});
