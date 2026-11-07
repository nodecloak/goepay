import { test, expect } from "@playwright/test";
import { publicSiteFixture191 } from "./fixtures/site191";

for (const width of [375, 768, 1440, 3840]) {
  for (const theme of ["light", "dark"]) {
    test(`policy and shared form layout ${width} ${theme}`, async ({
      page,
    }, info) => {
      await page.setViewportSize({ width, height: 1100 });
      await page.addInitScript(
        (theme) => localStorage.setItem("goepay-console-theme", theme),
        theme,
      );
      const errors: string[] = [];
      let writes = 0;
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/console/v1/**", (route) => {
        const site = publicSiteFixture191(route);
        if (site) return site;
        const path = new URL(route.request().url()).pathname;
        if (route.request().method() !== "GET") writes++;
        let data: unknown = {};
        if (path.endsWith("/session"))
          data = {
            user: {
              id: "1",
              name: "Local",
              role: "admin",
              admin_class: "super",
            },
            csrf_token: "local-layout",
          };
        if (path.includes("/referral-policies/")) {
          const scope = path.includes("/group/") ? "group" : "global";
          data = {
            id: "0",
            scope,
            scope_id: scope === "group" ? "12" : "0",
            revision: "0",
            enabled: null,
            rate_bps: null,
            mode: "immediate",
            basis: "fee",
            refund_policy: "any_successful_refund_voids_reward",
            membership_referral_policy_available: true,
          };
        }
        if (path.includes("/membership-referral-policies/"))
          data = {
            available: true,
            id: "0",
            scope: "global",
            scope_id: "0",
            revision: "0",
            enabled: null,
            rate_bps: null,
          };
        return route.fulfill({ json: { code: 0, data } });
      });
      await page.goto("/admin/system/referral-policies");
      const orders = page.getByRole("region", {
        name: "订单返现政策",
        exact: true,
      });
      const membership = page.getByRole("region", {
        name: "会员推广奖励政策",
        exact: true,
      });
      await expect(membership.getByLabel("会员奖励比例 BPS")).toBeVisible();
      for (const control of [
        orders.getByRole("button", { name: "奖励基数", exact: true }),
        membership.getByLabel("政策范围"),
        membership.getByLabel("启用状态"),
      ]) {
        const box = await control.boundingBox();
        expect(box!.width).toBeLessThanOrEqual(321);
        expect(box!.width).toBeGreaterThan(90);
      }
      const a = await orders.boundingBox(),
        b = await membership.boundingBox();
      if (width >= 1440) expect(Math.abs(a!.y - b!.y)).toBeLessThan(2);
      else expect(b!.y).toBeGreaterThan(a!.y);
      await orders
        .getByRole("button", { name: "启用状态", exact: true })
        .click();
      await orders.getByRole("option", { name: "启用", exact: true }).click();
      await membership.getByLabel("启用状态").selectOption("enabled");
      await membership.getByLabel("会员奖励比例 BPS").fill("500");
      await membership
        .getByRole("button", { name: "核对会员奖励政策", exact: true })
        .click();
      await membership
        .getByRole("button", { name: "返回编辑", exact: true })
        .click();
      await expect(membership.getByLabel("会员奖励比例 BPS")).toHaveValue(
        "500",
      );
      expect(writes).toBe(0);
      // Shared CSS contract: compact filters, bounded config controls, full
      // editor space and modal controls independent of the page width cap.
      const sizes = await page.evaluate(() => {
        const el = document.createElement("section");
        el.className = "panel";
        el.innerHTML =
          '<form class="console-config-form"><label>状态<select><option>关闭</option></select></label><label>网址<input type="url"></label><textarea></textarea></form><form class="form-grid"><label>筛选<select><option>全部</option></select></label></form><div class="modal" style="position:static;width:600px;max-width:100%"><select><option>长选择项</option></select></div>';
        document.querySelector(".content")!.append(el);
        const size = (selector: string) =>
          el.querySelector(selector)!.getBoundingClientRect().width;
        const result = {
          select: size("form select"),
          number: size("input"),
          text: size("textarea"),
          filter: size(".form-grid label"),
          modal: size(".modal select"),
          modalParent: size(".modal"),
        };
        el.remove();
        return result;
      });
      expect(sizes.select).toBeLessThanOrEqual(321);
      expect(sizes.filter).toBeLessThanOrEqual(321);
      if (width >= 1440) {
        expect(sizes.text).toBeGreaterThan(640);
        expect(sizes.modal).toBeGreaterThan(320);
      }
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      ).toBe(true);
      expect(errors).toEqual([]);
      await page.screenshot({
        path: info.outputPath("policies.png"),
        fullPage: true,
      });
    });
  }
}
