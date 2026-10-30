import { test, expect } from "@playwright/test";
const response = {
  items: [
    { name: "广东省", money: "90071992547409.93", count: "9007199254740993" },
    { name: "北京市", money: "2.00", count: "1" },
    { name: "未知", money: "3.00", count: "1" },
  ],
  total_money: "90071992547414.93",
  total_count: "9007199254740995",
  unknown_money: "3.00",
  unknown_count: "1",
};
for (const width of [1280, 375]) {
  for (const role of ["admin", "merchant"]) {
    test(`original ${role} province map and ranking use scoped exact facts at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      const reads: URL[] = [],
        writes: string[] = [];
      const mapAssets: string[] = [];
      page.on("request", (request) => {
        if (
          /ProvinceStatisticsReport|province-boundaries/.test(
            new URL(request.url()).pathname,
          )
        )
          mapAssets.push(request.url());
      });
      await page.clock.setFixedTime(new Date("2026-10-25T16:01:00Z"));
      await page.route("**/console/v1/**", (route) => {
        const req = route.request(),
          u = new URL(req.url());
        if (req.method() !== "GET") writes.push(u.pathname);
        let data: unknown = { items: [], total: 0 };
        if (u.pathname.endsWith("/session"))
          data = {
            user: {
              id: "1",
              role,
              admin_class: role === "admin" ? "super" : undefined,
            },
            csrf_token: "province136",
          };
        else if (u.pathname.endsWith("/reports/provinces")) {
          reads.push(u);
          data = response;
        }
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      });
      if (role === "admin" && width === 1280) {
        await page.goto("/admin/trade/order");
        await expect(page.locator("main")).toBeVisible();
        expect(mapAssets).toEqual([]);
      }
      await page.goto(
        role === "admin"
          ? "/admin/trade/stat/province"
          : "/merchant/trade/province-stat",
      );
      await expect(
        page.getByRole("group", { name: "地区交易分布地图" }),
      ).toBeVisible();
      const labels = page.locator(".province-map svg text");
      // The dark map now labels only data-bearing / ranked provinces (declutter):
      // the fixture carries 广东省 and 北京市, so two labels render.
      await expect(labels).toHaveCount(2);
      await expect(labels.filter({ hasText: /^广东省$/ })).toBeVisible();
      expect(
        await labels.evaluateAll((nodes) =>
          nodes.every((n) => getComputedStyle(n).pointerEvents === "none"),
        ),
      ).toBe(true);
      await expect(
        page.getByRole("heading", { name: "Top 10 省份" }),
      ).toBeVisible();
      await expect(
        page.getByText("90071992547414.93", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("9007199254740995", { exact: true }),
      ).toBeVisible();
      await expect.poll(() => reads.length).toBe(1);
      expect(reads[0].searchParams.get("view")).toBe("original");
      for (const label of [
        "今天",
        "昨天",
        "近3天",
        "近7天",
        "近15天",
        "近30天",
      ])
        await expect(
          page.getByRole("button", { name: label, exact: true }),
        ).toBeVisible();
      await page.getByRole("button", { name: "近3天", exact: true }).click();
      await expect.poll(() => reads.length).toBe(2);
      expect(reads[1].searchParams.get("from")).toBe("2026-10-24");
      expect(reads[1].searchParams.get("to")).toBe("2026-10-26");
      const guangdong = page.getByRole("button", { name: /^广东省：/ });
      await guangdong.focus();
      await guangdong.press("Enter");
      await expect(
        page.getByRole("status", { name: "选中地区" }),
      ).toContainText("90071992547409.93");
      await expect(page.getByText(/未匹配地区.*3\.00/)).toBeVisible();
      await page.getByText("地区数据明细", { exact: true }).click();
      await expect(
        page.getByRole("cell", { name: "未知", exact: true }),
      ).toBeVisible();
      await page.getByRole("button", { name: "放大地图", exact: true }).click();
      await expect(page.getByLabel("地图缩放比例")).toHaveText("125%");
      await page.getByRole("button", { name: "复位地图", exact: true }).click();
      await expect(page.getByLabel("地图缩放比例")).toHaveText("100%");
      if (role === "admin") {
        await page
          .getByLabel("商户号", { exact: true })
          .fill("9007199254740993");
        await page.getByLabel("通道ID", { exact: true }).fill("7");
        await page.getByRole("button", { name: "查询", exact: true }).click();
        await expect.poll(() => reads.length).toBe(3);
        expect(reads[2].searchParams.get("uid")).toBe("9007199254740993");
        expect(reads[2].searchParams.get("channel")).toBe("7");
      } else {
        await expect(page.getByLabel("商户号", { exact: true })).toHaveCount(0);
        await expect(page.getByLabel("通道ID", { exact: true })).toHaveCount(0);
        expect(
          reads.every(
            (u) => !u.searchParams.has("uid") && !u.searchParams.has("channel"),
          ),
        ).toBe(true);
      }
      expect(writes).toEqual([]);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ),
      ).toBe(false);
    });
  }
  test(`province errors and changed scopes cannot retain the old map totals at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    let calls = 0,
      release: () => void = () => {};
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/console/v1/**", async (route) => {
      const u = new URL(route.request().url());
      let data: unknown = { items: [], total: 0 };
      if (u.pathname.endsWith("/session"))
        data = {
          user: { id: "1", role: "admin", admin_class: "super" },
          csrf_token: "province136",
        };
      else if (u.pathname.endsWith("/reports/provinces")) {
        calls++;
        if (calls === 2) await held;
        if (calls === 3)
          return route.fulfill({
            status: 500,
            contentType: "application/json",
            body: JSON.stringify({ code: -1, message: "地区统计失败" }),
          });
        data = response;
      }
      try {
        await route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ code: 0, data }),
        });
      } catch {}
    });
    await page.goto("/admin/trade/stat/province");
    await expect(
      page.getByText("90071992547414.93", { exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "刷新数据", exact: true }).click();
    await expect.poll(() => calls).toBe(2);
    await page.getByLabel("商户号", { exact: true }).fill("2");
    release();
    await expect(
      page.getByText("90071992547414.93", { exact: true }),
    ).toHaveCount(0);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("地区统计失败");
    await expect(
      page.getByText("90071992547414.93", { exact: true }),
    ).toHaveCount(0);
  });
}
