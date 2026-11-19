import { expect, test, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { accountSettingsReadFixture } from "./fixtures/account-settings";

const merchantID = "1000";
const orderNo = "SYNTHETIC0123456789012345678901234567890123456";
const fingerprint = "a".repeat(64);
const date = "2026-11-19T03:30:00Z";
const record = {
  id: "27",
  domain: "merchant-check.example.invalid",
  name: "本地进件资料",
  status: "pending",
};
const onboardingFields = [
  { name: "business_name", label: "经营名称", max_length: 200, required: true },
  { name: "contact_name", label: "联系人", max_length: 100, required: true },
  { name: "contact_phone", label: "联系电话", max_length: 40, required: true },
  { name: "channel_id", label: "进件渠道 ID", max_length: 19, required: true },
  { name: "remark", label: "备注", max_length: 2000 },
].map((field) => ({ ...field, type: "string" }));
type MenuPage = { path: string; title: string };
type MenuNode = {
  path: string;
  meta: { title: string };
  children?: MenuNode[];
};
function collectPages(menu: MenuNode[], parent = ""): MenuPage[] {
  return menu.flatMap((node) => {
    const path = node.path.startsWith("/")
      ? node.path
      : parent + "/" + node.path;
    return node.children?.length
      ? collectPages(node.children, path)
      : [
          {
            path,
            title: path === "/account/api-info" ? "API 接入" : node.meta.title,
          },
        ];
  });
}
const menu = JSON.parse(
  readFileSync(
    new URL("../src/data/menu-merchant.json", import.meta.url),
    "utf-8",
  ),
) as MenuNode[];
const pages = [
  ...collectPages(menu),
  { path: "/account/notice", title: "消息提醒设置" },
  { path: "/account/printing", title: "订单打印设置" },
  { path: "/account/settlement-schedule", title: "自动结算参与设置" },
  { path: "/account/crypto", title: "资金概览" },
  { path: "/account/crypto/invoices", title: "USDT 收款" },
  { path: "/account/crypto/settlements", title: "USDT 结算" },
  { path: "/account/crypto/refunds", title: "USDT 退币" },
  { path: "/account/crypto/recipients", title: "结算与退币地址" },
  { path: "/transfer/batches", title: "批量付款" },
  { path: "/transfer/red", title: "创建红包" },
  { path: "/trade/order?type=usdt", title: "订单管理" },
  { path: "/applyments/form?id=27", title: "商户进件表单" },
  { path: "/scanpay", title: "付款码收款" },
];

async function fixture(page: Page, baseURL: string | undefined) {
  const origin = new URL(baseURL || "").origin;
  if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(origin))
    throw Error("Merchant workspace verification uses loopback only");
  const writes: string[] = [];
  const foreign: string[] = [];
  const faults: string[] = [];
  page.on("pageerror", (error) => faults.push(error.message));
  await page.context().route("**/*", (route) => {
    const target = new URL(route.request().url());
    if (target.origin === origin) return route.continue();
    foreign.push(target.origin);
    return route.abort();
  });
  await page.route("**/console/v1/**", (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    let data: unknown = accountSettingsReadFixture(path) ?? {
      items: [],
      total: 0,
    };
    if (request.method() !== "GET") {
      writes.push(path);
      return route.abort();
    }
    if (path.endsWith("/session"))
      data = {
        user: { id: merchantID, role: "merchant", name: "本地商户" },
        csrf_token: "merchant-workspace-fixture",
      };
    else if (path.endsWith("/dashboard"))
      data = {
        merchant: { id: merchantID, name: "本地商户", enabled: true, pay: 1 },
        summary: {
          today_orders: "2",
          today_paid_orders: "1",
          today_money: "66.50",
          yesterday_money: "199.20",
          today_transfer_money: "30.00",
          yesterday_transfer_money: "15.00",
          settled_money: "858.00",
        },
        available_money: "8726.50",
        frozen_money: "25.00",
        announcements: [],
        trend: [
          "117.70",
          "68.50",
          "218.65",
          "177.80",
          "128.50",
          "199.20",
          "66.50",
        ].map((amount, i) => ({
          date: "2026-11-" + (13 + i),
          orders: String(i + 2),
          paid_orders: String(i + 1),
          amount,
        })),
        payment_mix: [{ type: "alipay", orders: "1", amount: "66.50" }],
      };
    else if (path.endsWith("/account/api-info"))
      data = {
        pid: merchantID,
        algorithm: "MD5",
        api_key_masked: "********test",
        merchant_public_key: "",
        platform_public_key: "",
        rsa_fingerprint: fingerprint,
        can_rotate_rsa: true,
      };
    else if (path.endsWith("/signature-policy"))
      data = {
        merchant_id: merchantID,
        algorithm: "MD5",
        keytype: 0,
        rsa_configured: false,
        fingerprint,
        can_update: true,
      };
    else if (path.endsWith("/event-subscription"))
      data = {
        event_url: "",
        event_types: [],
        fingerprint,
        consecutive_failures: 0,
        last_delivery_state: "none",
        subscription_notice: "",
      };
    else if (path.endsWith("/account/profile"))
      data = {
        name: "本地商户",
        uid: merchantID,
        has_pwd: true,
        registration_time_known: true,
        addtime: "2026-01-01 00:00:00",
        pay: "1",
      };
    else if (path.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (path.endsWith("/crypto/config"))
      data = { configured: false, enabled: false, networks: [] };
    else if (path.endsWith("/collection-generator"))
      data = {
        recipient_name: "本地商户",
        default_codename: "本地商户",
        codename_customized: false,
        styles: [],
        domains: [],
        latest: null,
      };
    else if (path.endsWith("/merchant/orders"))
      data = {
        total: 1,
        items: [
          {
            id: "123456789012345678",
            trade_no: orderNo,
            out_trade_no: "DEMO-MERCHANT-001",
            name: "本地订单",
            amount: "6650",
            realmoney: "6650",
            type: "alipay",
            status: "pending",
            created_at: date,
          },
        ],
      };
    else if (
      /\/resources\/(domains|onboarding-applications|merchant-risk)\/schema$/.test(
        path,
      )
    )
      data = {
        title: path.includes("/domains/") ? "授权域名" : "商户进件",
        fields: path.includes("/onboarding-applications/")
          ? onboardingFields
          : [
              { name: "domain", label: "域名", type: "string" },
              { name: "name", label: "名称", type: "string", required: true },
              {
                name: "status",
                label: "审核状态",
                type: "string",
                read_only: true,
              },
            ],
        permissions: { create: true, update: true, delete: false },
      };
    else if (
      /\/resources\/(domains|onboarding-applications|merchant-risk)$/.test(path)
    )
      data = { items: [record], total: 1 };
    else if (path.endsWith("/resources/onboarding-applications/27"))
      data = record;
    return route.fulfill({ json: { code: 0, data } });
  });
  return { writes, foreign, faults };
}

for (const [width, theme] of [
  [1440, "light"],
  [375, "dark"],
] as const) {
  for (const { path, title } of pages) {
    test(`${theme} ${width}px opens merchant ${path} without document overflow`, async ({
      page,
      baseURL,
    }, testInfo) => {
      const calls = await fixture(page, baseURL);
      await page.addInitScript((value) => {
        localStorage.setItem("goepay-console-theme", value);
      }, theme);
      await page.setViewportSize({ width, height: 900 });
      await page.goto("/merchant" + path);
      const main = page.getByRole("main");
      await expect(
        main.getByRole("heading", { level: 1, name: title, exact: true }),
      ).toBeVisible();
      await expect(main.getByRole("heading", { level: 1 })).toHaveCount(1);
      await page.waitForLoadState("networkidle");
      await expect(
        main.getByText("您没有访问此页面的权限", { exact: true }),
      ).toHaveCount(0);
      await expect(main.getByText("未找到该页面", { exact: true })).toHaveCount(
        0,
      );
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      expect(calls.faults).toEqual([]);
      expect(calls.foreign).toEqual([]);
      expect(calls.writes).toEqual([]);
      if (
        path === "/applyments/create" ||
        path.startsWith("/applyments/form")
      ) {
        const form = main.locator("form");
        const bounds = await Promise.all(
          onboardingFields.map(async ({ label }) => {
            const control = form.getByLabel(label, { exact: true });
            await expect(control).toBeVisible();
            return (await control.boundingBox())!;
          }),
        );
        const [business, contact, phone, channel, remark] = bounds;
        expect(bounds.map(({ x }) => x)).toEqual(bounds.map(() => business.x));
        if (width > 600) {
          expect(business.width).toBeLessThanOrEqual(640);
          expect(remark.width).toBe(business.width);
          expect(contact.width).toBeLessThan(business.width);
          expect(phone.width).toBeLessThanOrEqual(240);
          expect(channel.width).toBe(phone.width);
        } else {
          expect(business.width).toBeGreaterThan(250);
          expect(bounds.map(({ width }) => width)).toEqual(
            bounds.map(() => business.width),
          );
        }
      }
      if (
        [
          "/dashboard",
          "/trade/order",
          "/account/api-info",
          "/account/crypto",
        ].includes(path)
      )
        await page.screenshot({
          path: testInfo.outputPath("merchant-view.png"),
          animations: "disabled",
        });
    });
  }
}

test("mobile order records keep long IDs, field labels and bulk selection usable", async ({
  page,
  baseURL,
}) => {
  const calls = await fixture(page, baseURL);
  await page.setViewportSize({ width: 375, height: 900 });
  await page.goto("/merchant/trade/order");
  const searchFields = page.getByRole("form", { name: "筛选记录" });
  const searchMode = searchFields.getByRole("button", {
    name: "订单搜索字段",
    exact: true,
  });
  const searchContent = searchFields.getByLabel("搜索内容", { exact: true });
  await expect(searchMode).toBeVisible();
  await expect(searchContent).toBeVisible();
  const left = await searchMode.boundingBox();
  const right = await searchContent.boundingBox();
  expect(left!.x + left!.width).toBeLessThan(right!.x);
  const table = page.getByRole("table", { name: "订单列表" });
  const row = table.getByRole("row").filter({ hasText: orderNo });
  await expect(row.getByText("本地订单", { exact: true })).toBeVisible();
  const selector = table.getByRole("checkbox", {
    name: "全选本页订单",
    exact: true,
  });
  await expect(selector).toBeVisible();
  await selector.check();
  await expect(row.getByRole("checkbox")).toBeChecked();
  await selector.uncheck();
  await expect(row.getByRole("checkbox")).not.toBeChecked();
  expect(await row.evaluate((node) => getComputedStyle(node).display)).toBe(
    "grid",
  );
  expect(calls.writes).toEqual([]);
});
