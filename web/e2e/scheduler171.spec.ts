import { publicSiteFixture191 } from "./fixtures/site191";
import { test, expect, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
const fixtures = JSON.parse(
  readFileSync(
    new URL("./scheduler171-http-fixtures.json", import.meta.url),
    "utf8",
  ),
) as Record<string, any>;
async function setup(
  page: Page,
  options: {
    manual?: boolean;
    operator?: boolean;
    delegated?: boolean;
    defer?: boolean;
    conflict?: boolean;
  } = {},
) {
  const writes: any[] = [],
    unexpected: string[] = [],
    reads: string[] = [];
  let current = structuredClone(
      fixtures[options.manual ? "manual_before_task" : "completed_task"],
    ),
    release = () => {};
  const pending = new Promise<void>((r) => (release = r));
  await page.route("**/console/v1/**", async (route) => {
    const siteReply191 = publicSiteFixture191(route);
    if (siteReply191) return siteReply191;
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname;
    let data: unknown = {},
      status = 200;
    if (path === "/console/v1/public/layout") {
      expect(request.method()).toBe("GET");
      data = { zhuce: "", footer: "", format: "html" };
    } else if (path.endsWith("/session"))
      data = {
        user: {
          id: "9007199254740993",
          name: "本地合成管理员",
          role: "admin",
          admin_class: options.operator ? "operator" : "super",
        },
        csrf_token: "scheduler171-browser",
        ...(options.delegated
          ? { delegated_by: { id: "171", name: "Synthetic delegated" } }
          : {}),
      };
    else if (path.endsWith("/scheduled-tasks")) {
      reads.push(path);
      const catalog = structuredClone(
        fixtures[
          options.manual ? "manual_before_catalog" : "completed_catalog"
        ],
      );
      catalog.items = catalog.items.map((t: any) =>
        t.task === current.task ? current : t,
      );
      data = catalog;
    } else if (path.endsWith("/scheduled-tasks/referral_daily")) {
      if (request.method() === "PATCH") {
        writes.push(request.postDataJSON());
        if (options.defer) await pending;
        if (options.conflict) {
          status = 409;
          data = null;
        } else {
          expect(request.postDataJSON().expected_fingerprint).toBe(
            current.fingerprint,
          );
          expect(request.postDataJSON().enabled).toBe(false);
          current = structuredClone(fixtures.saved_disabled_task);
          data = current;
        }
      } else data = current;
    } else if (path.endsWith("/scheduled-tasks/referral_daily/run")) {
      writes.push(request.postDataJSON());
      if (options.defer) await pending;
      expect(request.postDataJSON()).toEqual({
        expected_fingerprint: current.fingerprint,
      });
      current = structuredClone(fixtures.manual_pending_task);
      data = fixtures.manual_pending_run;
    } else if (path.endsWith("/scheduled-tasks/referral_daily/runs")) {
      expect(url.searchParams.get("before_id")).toBe("0");
      data = options.manual
        ? fixtures.manual_pending_history
        : fixtures.completed_history;
    } else {
      unexpected.push(request.method() + " " + path);
      status = 404;
      data = null;
    }
    await route.fulfill({
      status,
      json: {
        code: status === 200 ? 0 : -1,
        message: status === 409 ? "changed" : "",
        data,
      },
    });
  });
  await page.goto("/admin/system/cron");
  return {
    writes,
    unexpected,
    reads,
    release,
    row: page
      .locator("article.job-card")
      .filter({ has: page.getByText("每日邀请返现结算", { exact: true }) }),
  };
}
for (const width of [1440, 375])
  test(`original scheduler configuration, cancellation, real history and responsive fields ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 900 });
    const f = await setup(page, { defer: true });
    await expect(
      page.getByRole("region", { name: "计划任务", exact: true }),
    ).toBeVisible();
    const notify = page
      .locator("article.job-card")
      .filter({ has: page.getByText("订单异步通知重试", { exact: true }) });
    await expect(notify).toContainText("未接管");
    await expect(notify).not.toContainText("已关闭");
    await f.row.getByRole("button", { name: "配置", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(
      dialog.getByLabel("执行时间（UTC+8）", { exact: true }),
    ).toBeVisible();
    await dialog.getByLabel("开启此任务", { exact: true }).uncheck();
    await dialog.getByRole("button", { name: "核对配置", exact: true }).click();
    expect(f.writes).toHaveLength(0);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(
      f.row.getByRole("button", { name: "配置", exact: true }),
    ).toBeFocused();
    await f.row.getByRole("button", { name: "配置", exact: true }).click();
    await dialog.getByLabel("开启此任务", { exact: true }).uncheck();
    await dialog.getByRole("button", { name: "核对配置", exact: true }).click();
    await page.screenshot({
      path: info.outputPath(`scheduler171-confirm-${width}.png`),
    });
    await dialog.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect(
      dialog.getByRole("button", { name: "关闭", exact: true }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeVisible();
    f.release();
    await expect(dialog.getByRole("status")).toContainText("任务配置已保存");
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await expect(f.row).toContainText("已关闭");
    expect(f.writes).toHaveLength(1);
    await f.row.getByRole("button", { name: "执行记录", exact: true }).click();
    await expect(
      dialog.getByText("9007199254740993", { exact: true }),
    ).toBeVisible();
    await expect(dialog.getByText("真实完成", { exact: true })).toBeVisible();
    await expect(dialog).toContainText("本轮已完成");
    await page.screenshot({
      path: info.outputPath(`scheduler171-history-${width}.png`),
    });
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(f.unexpected).toEqual([]);
  });
test("manual action uses actual pending HTTP fixture and cannot claim success before execution", async ({
  page,
}) => {
  const f = await setup(page, { manual: true, defer: true });
  await f.row.getByRole("button", { name: "立即运行", exact: true }).click();
  const dialog = page.getByRole("dialog");
  expect(f.writes).toHaveLength(0);
  await dialog.getByRole("button", { name: "确认安排", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "关闭", exact: true }),
  ).toBeDisabled();
  f.release();
  await expect(dialog.getByRole("status")).toContainText("等待调度器执行");
  await expect(dialog).toContainText(fixtures.manual_pending_run.id);
  await expect(dialog).not.toContainText("执行成功");
  expect(f.writes).toHaveLength(1);
  expect(f.unexpected).toEqual([]);
});
test("CAS conflict requires explicit fresh read and never repeats the request", async ({
  page,
}) => {
  const f = await setup(page, { conflict: true });
  await f.row.getByRole("button", { name: "配置", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("开启此任务", { exact: true }).uncheck();
  await dialog.getByRole("button", { name: "核对配置", exact: true }).click();
  await dialog.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("刷新核对");
  await expect(
    dialog.getByRole("button", { name: "核对配置", exact: true }),
  ).toBeDisabled();
  expect(f.writes).toHaveLength(1);
  await dialog.getByRole("button", { name: "刷新核对", exact: true }).click();
  await expect(dialog.getByLabel("开启此任务", { exact: true })).toBeChecked();
  expect(f.writes).toHaveLength(1);
  expect(f.unexpected).toEqual([]);
});
for (const permission of ["operator", "delegated"] as const)
  test(`only original super may read task policies: ${permission}`, async ({
    page,
  }) => {
    const f = await setup(page, { [permission]: true });
    if (permission === "operator")
      await expect(
        page.getByRole("heading", {
          name: "您没有访问此页面的权限",
          exact: true,
        }),
      ).toBeVisible();
    else
      await expect(
        page.getByText("仅本人原超级管理员会话可以核对和配置计划任务。", {
          exact: true,
        }),
      ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "配置", exact: true }),
    ).toHaveCount(0);
    expect(f.reads).toHaveLength(0);
    expect(f.writes).toHaveLength(0);
    expect(f.unexpected).toEqual([]);
  });
