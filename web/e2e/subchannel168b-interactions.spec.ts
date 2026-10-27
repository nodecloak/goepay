import { test, expect, type Page } from "@playwright/test";
import fs from "node:fs";
const fixture = JSON.parse(
  fs.readFileSync(
    new URL("./subchannel168b-fixture.json", import.meta.url),
    "utf8",
  ),
);
const id = fixture.merchant.items[0].id;
async function setup(page: Page, role: "merchant" | "admin") {
  const writes: { method: string; path: string; body: any }[] = [];
  const rows = structuredClone(fixture[role].items);
  const policy = structuredClone(fixture.policy);
  let release: (() => void) | undefined;
  let delayed = "";
  let abortCreate = false;
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", async (route) => {
    const r = route.request(),
      u = new URL(r.url());
    if (!["127.0.0.1", "localhost"].includes(u.hostname)) return route.abort();
    if (!u.pathname.startsWith("/console/v1/")) return route.continue();
    let data: any = { items: [], total: 0 };
    const path = u.pathname.replace(`/console/v1/${role}`, "");
    if (path === "/session")
      data = {
        user: {
          id: role === "admin" ? "168" : "1",
          name: "合成168B",
          role,
          ...(role === "admin" ? { admin_class: "super" } : {}),
        },
        csrf_token: "subchannel168b-browser-csrf",
      };
    else if (path === "/payment-subchannels/schemas") data = fixture.schemas;
    else if (path === "/payment-subchannels/policy") data = policy;
    else if (path === "/payment-subchannels")
      data = { ...fixture[role], items: rows, total: rows.length };
    else if (path === `/payment-subchannels/${id}`) data = rows[0];
    else if (path === `/payment-subchannels/${id}/collection-link`)
      data = {
        url: `https://synthetic.invalid/collect/1?type=alipay&subchannel_id=${id}&expires=2000000000&token=synthetic168b`,
        expires_at: "2030-01-01T00:00:00Z",
      };
    else if (path === `/payment-subchannels/${id}/test`)
      data = {
        trade_no: "T168B_ORIGINAL",
        state: "unknown",
        url: "https://synthetic.invalid/checkout/T168B_ORIGINAL?expires=2000000000&token=synthetic168b",
      };
    if (!["GET", "HEAD"].includes(r.method())) {
      const body = r.postDataJSON();
      writes.push({ method: r.method(), path: path + u.search, body });
      if (path === delayed)
        await new Promise<void>((resolve) => {
          release = resolve;
        });
      if (path === "/payment-subchannels/policy") {
        policy.roll_type = body.roll_type;
        policy.revision = String(BigInt(policy.revision) + 1n);
        policy.fingerprint = "b".repeat(64);
        data = policy;
      } else if (r.method() === "DELETE") {
        data = {
          ...rows[0],
          enabled: false,
          archived: true,
          browser_test_available: false,
          unavailable_reason: "disabled",
          revision: String(BigInt(rows[0].revision) + 1n),
          fingerprint: "c".repeat(64),
        };
        rows.shift();
      } else if (path === `/payment-subchannels/${id}`) {
        for (const k of [
          "name",
          "enabled",
          "weight",
          "min_amount",
          "max_amount",
        ])
          if (body[k] !== undefined) rows[0][k] = body[k];
        rows[0].revision = String(BigInt(rows[0].revision) + 1n);
        rows[0].fingerprint = "b".repeat(64);
        if (body.values)
          rows[0].account_version = String(
            BigInt(rows[0].account_version) + 1n,
          );
        if (rows[0].enabled === false) {
          rows[0].browser_test_available = false;
          rows[0].unavailable_reason = "disabled";
        }
        data = rows[0];
      } else if (path === "/payment-subchannels") {
        const created = {
          ...structuredClone(fixture[role].items[1]),
          id: "9007199254740995",
          merchant_id: body.merchant_id,
          parent_id: body.parent_id,
          name: body.name,
          enabled: body.enabled,
          weight: body.weight,
          min_amount: body.min_amount,
          max_amount: body.max_amount,
          revision: "1",
          account_version: "1",
          fingerprint: "e".repeat(64),
        };
        rows.push(created);
        data = created;
        if (abortCreate) {
          abortCreate = false;
          return route.abort("failed");
        }
      }
    }
    return route.fulfill({ status: 200, json: { code: 0, message: "", data } });
  });
  await page.goto(
    role === "admin" ? "/admin/applyments/subchannel" : "/merchant/subchannel",
  );
  await expect(page.getByText(rows[0].name, { exact: true })).toBeVisible();
  return {
    writes,
    errors,
    abortNextCreate: () => {
      abortCreate = true;
    },
    delay: (path: string) => {
      delayed = path;
    },
    release: () => release?.(),
  };
}
async function action(page: Page, name: string) {
  await page
    .getByRole("button", { name: `子通道 ${id} 操作`, exact: true })
    .click();
  await page.getByRole("menuitem", { name, exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
}
for (const width of [1440, 375]) {
  test(`subchannel168b merchant full controls, focus and uncertain once ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const s = await setup(page, "merchant");
    const opener = page.getByRole("button", { name: "轮询设置", exact: true });
    await opener.click();
    await page
      .getByRole("dialog")
      .getByRole("combobox", { name: "轮询方式", exact: true })
      .selectOption("1");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    await expect(opener).toBeFocused();
    expect(s.writes).toHaveLength(0);
    await action(page, "单笔限额设置");
    await page
      .getByRole("dialog")
      .getByLabel("单笔订单金额下限（元）", { exact: true })
      .fill("0.01");
    await page
      .getByRole("dialog")
      .getByLabel("单笔订单金额上限（元）", { exact: true })
      .fill("1.02");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认保存", exact: true })
      .click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(s.writes[0].body.min_amount).toBe("1");
    expect(s.writes[0].body.max_amount).toBe("102");
    await action(page, "收款链接");
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "读取收款链接", exact: true })
      .click();
    await expect(
      page.getByRole("dialog").getByLabel("完整收款链接", { exact: true }),
    ).toHaveValue(new RegExp(`subchannel_id=${id}`));
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    expect(s.writes).toHaveLength(1);
    await action(page, "支付测试");
    await expect(
      page.getByRole("dialog").getByLabel("测试金额（元）", { exact: true }),
    ).toHaveValue("1.00");
    s.delay(`/payment-subchannels/${id}/test`);
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认生成一次支付测试", exact: true })
      .click();
    await expect(
      page
        .getByRole("dialog")
        .getByRole("button", { name: "关闭并保留原订单", exact: true }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect.poll(() => s.writes.length).toBe(2);
    s.release();
    await expect(
      page
        .getByRole("dialog")
        .getByRole("link", { name: "打开原支付页面", exact: true }),
    ).toHaveAttribute("href", /T168B_ORIGINAL/);
    await expect(
      page
        .getByRole("dialog")
        .getByRole("button", { name: "确认生成一次支付测试", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "核对本次测试", exact: true })
      .click();
    expect(s.writes.filter((w) => w.path.endsWith("/test"))).toHaveLength(1);
    await page.screenshot({
      path: `../.tools/subchannel168b-merchant-unknown-${width}.png`,
      animations: "disabled",
    });
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "关闭并保留原订单", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: `子通道 ${id} 操作`, exact: true }),
    ).toBeFocused();
    await page
      .getByRole("button", { name: `子通道 ${id} 操作`, exact: true })
      .click();
    await expect(
      page.getByRole("menuitem", { name: "支付订单", exact: true }),
    ).toHaveAttribute("href", `/merchant/trade/order?subchannel=${id}`);
    await expect(
      page.getByRole("menuitem", { name: "删除子通道", exact: true }),
    ).toHaveCount(0);
    await page.keyboard.press("Escape");
    expect(s.errors).toEqual([]);
    await info.attach("subchannel168b-requests", {
      body: JSON.stringify(s.writes),
      contentType: "application/json",
    });
  });
  test(`subchannel168b admin original account create, replace, archive ${width}`, async ({
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 });
    const s = await setup(page, "admin");
    await page.getByRole("button", { name: "新增子通道", exact: true }).click();
    const d = page.getByRole("dialog");
    await d.getByLabel("所属商户编号", { exact: true }).fill("1");
    await d.getByLabel("名称", { exact: true }).fill("本地合成管理员子通道😀");
    await d.getByLabel("app", { exact: true }).fill("synthetic-child-new-app");
    await d
      .getByLabel("secret", { exact: true })
      .fill("synthetic-child-new-secret");
    await d.getByLabel("单笔订单金额下限（元）", { exact: true }).fill("0.01");
    await d.getByLabel("单笔订单金额上限（元）", { exact: true }).fill("1.02");
    await page.screenshot({
      path: `../.tools/subchannel168b-admin-create-${width}.png`,
      animations: "disabled",
    });
    await d.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect(d).toHaveCount(0);
    expect(s.writes[0].method).toBe("POST");
    expect(s.writes[0].body).toMatchObject({
      merchant_id: "1",
      parent_id: "7",
      min_amount: "1",
      max_amount: "102",
      enabled: false,
    });
    await action(page, "参数设置");
    await expect(d.locator("textarea")).toHaveCount(0);
    await d
      .locator(".subchannel-param")
      .first()
      .locator("select")
      .selectOption("replace");
    await d.getByLabel("app", { exact: true }).fill("synthetic-replacement");
    await d.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect(d).toHaveCount(0);
    expect(s.writes[1].body.values).toMatchObject({
      app: { operation: "replace", value: "synthetic-replacement" },
      secret: { operation: "keep" },
    });
    expect(await page.locator("body").textContent()).not.toContain(
      "synthetic-replacement",
    );
    await action(page, "删除子通道");
    await d.getByRole("button", { name: "取消", exact: true }).click();
    expect(s.writes).toHaveLength(2);
    await action(page, "删除子通道");
    s.delay(`/payment-subchannels/${id}`);
    await d.getByRole("button", { name: "确认删除", exact: true }).click();
    await page.keyboard.press("Escape");
    await expect(d).toBeVisible();
    await expect.poll(() => s.writes.length).toBe(3);
    s.release();
    await expect(d).toHaveCount(0);
    expect(s.writes[2]).toMatchObject({
      method: "DELETE",
      path: `/payment-subchannels/${id}`,
    });
    expect(Object.keys(s.writes[2].body)).toEqual(["expected_fingerprint"]);
    expect(s.errors).toEqual([]);
    await info.attach("subchannel168b-admin-requests", {
      body: JSON.stringify(s.writes),
      contentType: "application/json",
    });
  });
}

test("subchannel168b admin committed create response loss needs explicit read before any repeat", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 1000 });
  const s = await setup(page, "admin");
  s.abortNextCreate();
  await page.getByRole("button", { name: "新增子通道", exact: true }).click();
  const d = page.getByRole("dialog");
  await d.getByLabel("所属商户编号", { exact: true }).fill("1");
  await d.getByLabel("名称", { exact: true }).fill("合成未知新增必须先核对");
  await d.getByLabel("app", { exact: true }).fill("synthetic-aborted-app");
  await d
    .getByLabel("secret", { exact: true })
    .fill("synthetic-aborted-secret");
  await d.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect.poll(() => s.writes.length).toBe(1);
  await expect(
    d.getByRole("button", { name: "确认保存", exact: true }),
  ).toBeDisabled();
  await expect(
    d.getByRole("button", { name: "读取最新列表核对", exact: true }),
  ).toBeVisible();
  await d
    .getByRole("button", { name: "读取最新列表核对", exact: true })
    .click();
  await expect(d).toHaveCount(0);
  await expect(
    page.getByText("合成未知新增必须先核对", { exact: true }),
  ).toBeVisible();
  expect(s.writes).toHaveLength(1);
  expect(await page.locator("body").textContent()).not.toContain(
    "synthetic-aborted-secret",
  );
});
