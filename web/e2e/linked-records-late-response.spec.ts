import { test, expect } from "@playwright/test";
import { settlementListFixture } from "../tests/fixtures/settlement-list";
for (const width of [1280, 375])
  for (const [path, api] of [
    ["/admin/settle/list", "/workflows/settlements"],
    ["/admin/transfer/list", "/transfers"],
  ])
    test(`old linked ${api} response is disposed before another merchant is shown at ${width}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 1000 });
      let release!: () => void, arrived!: () => void, finished!: () => void;
      const gate = new Promise<void>((r) => (release = r)),
        started = new Promise<void>((r) => (arrived = r)),
        done = new Promise<void>((r) => (finished = r)),
        writes: string[] = [];
      await page.route("**/console/v1/**", async (route) => {
        const request = route.request(),
          url = new URL(request.url());
        if (request.method() !== "GET") writes.push(url.pathname);
        let data: unknown = { items: [], total: 0 };
        if (url.pathname.endsWith("/session"))
          data = {
            user: { id: "1", role: "admin", admin_class: "super" },
            csrf_token: "late149",
          };
        else if (url.pathname === "/console/v1/admin" + api) {
          const mid = url.searchParams.get("merchant_id"),
            row = {
              ...settlementListFixture(),
              id: mid === "1" ? "111" : "2",
              merchant_id: mid,
              type: "alipay",
            };
          if (mid === "1") {
            arrived();
            await gate;
            try {
              await route.fulfill({
                json: { code: 0, data: { items: [row], total: 1 } },
              });
            } catch {
            } finally {
              finished();
            }
            return;
          }
          data = { items: [row], total: 1 };
        }
        try {
          await route.fulfill({ json: { code: 0, data } });
        } catch {}
      });
      try {
        await page.goto(path + "?uid=1");
        await started;
        const current = page.waitForResponse(
          (r) =>
            new URL(r.url()).pathname === "/console/v1/admin" + api &&
            new URL(r.url()).searchParams.get("merchant_id") === "2",
        );
        await page.evaluate((next) => {
          history.pushState({ ...history.state, current: next }, "", next);
          dispatchEvent(
            new PopStateEvent("popstate", { state: history.state }),
          );
        }, path + "?uid=2");
        await current;
        await expect(
          page.getByText("商户号：2", { exact: true }),
        ).toBeVisible();
        release();
        await done;
        await expect(page.locator("tbody")).not.toContainText("111");
        expect(writes).toEqual([]);
      } finally {
        release();
      }
    });
