import { pageReload248 } from "./fixtures/page-reload248";
import { test, expect, type Page } from "@playwright/test";
import { kycConfig233, kycInfo233 } from "../tests/fixtures/kyc233";

async function fixture(
  page: Page,
  area: "admin" | "merchant",
  options: {
    unknownInit?: boolean;
    unknownOCR?: boolean;
    corporate?: boolean;
    delegated?: boolean;
    callback?: string;
    unauthenticated?: boolean;
    wrongAttempt?: boolean;
  } = {},
) {
  let config = kycConfig233(),
    loggedIn = !options.unauthenticated,
    inboxReady = false,
    info = kycInfo233({
      cert_corpopen: !!options.corporate,
      ocr_type: "aliyun",
      ...(options.callback
        ? {
            cert_open: 3,
            attempt_id: options.wrongAttempt ? "b".repeat(32) : "a".repeat(32),
            status: "pending",
          }
        : {}),
    });
  const writes: { path: string; body: unknown }[] = [];
  const documents: string[] = [];
  page.on("request", (request) => {
    if (request.isNavigationRequest() && request.frame() === page.mainFrame())
      documents.push(new URL(request.url()).pathname);
  });
  await page.route("**/callbacks/certification233/return?**", async (route) => {
    const params = new URL(route.request().url()).searchParams;
    if (
      params.get("attempt_id") !== "a".repeat(32) ||
      params.get("state") !== "s".repeat(32) ||
      params.get("auth_code") !== "syntheticcode233"
    )
      return route.fulfill({
        status: 422,
        contentType: "text/html",
        body: "<p>本地模拟回跳被拒绝</p>",
      });
    inboxReady = true;
    return route.fulfill({
      status: 303,
      headers: {
        Location: "/merchant/account/certificate",
        "Cache-Control": "no-store",
      },
      body: "",
    });
  });
  await page.route("**/console/v1/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname,
      method = req.method();
    let data: unknown = {};
    if (path.endsWith("/session") && !loggedIn)
      return route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({
          code: -1,
          message: "authentication required",
          data: null,
        }),
      });
    if (path.endsWith("/session") || path.endsWith("/login")) {
      if (path.endsWith("/login")) {
        loggedIn = true;
        writes.push({ path, body: req.postDataJSON() });
      }
      data = {
        user: {
          id: area === "admin" ? "1" : options.wrongAttempt ? "1001" : "1000",
          role: area,
          admin_class: area === "admin" ? "super" : undefined,
          name: "本地验收233",
        },
        csrf_token: "synthetic233",
        delegated_by: options.delegated
          ? { id: "1", name: "本地管理员" }
          : undefined,
      };
    } else if (path.includes("/public/login-captcha?"))
      data = { enabled: false };
    else if (path.endsWith("/public/login-captcha")) data = { enabled: false };
    else if (path.endsWith("/public/merchant-access"))
      data = { close_keylogin: 1, user_refund: 1 };
    else if (path.endsWith("/public/connect/options"))
      data = {
        providers: { qq: false, wx: false, alipay: false, telegram: false },
      };
    else if (path.endsWith("/resources/settings/schema"))
      data = {
        title: "设置",
        fields: [{ name: "key", type: "select", options: ["site_name"] }],
        permissions: { create: true, update: true, delete: false },
      };
    else if (path.endsWith("/resources/settings"))
      data = {
        items: [{ id: "1", key: "site_name", value: "本地认证验收233" }],
        total: 1,
      };
    else if (
      path.endsWith("/settings/certification") ||
      path.endsWith("/settings/ocr")
    ) {
      if (method === "PUT") {
        const body = req.postDataJSON();
        writes.push({ path, body });
        if (body.expected_revision !== config.revision)
          return route.fulfill({
            status: 409,
            contentType: "application/json",
            body: JSON.stringify({
              code: -1,
              message: "状态已变化，请重新读取",
              data: null,
            }),
          });
        config = {
          ...config,
          ...body,
          revision: String(Number(config.revision) + 1),
          credentials: Object.fromEntries(
            Object.keys(config.credentials).map((key) => [
              key,
              {
                present:
                  body.credentials[key].action === "replace" ||
                  (body.credentials[key].action !== "clear" &&
                    config.credentials[key as keyof typeof config.credentials]
                      .present),
                masked:
                  body.credentials[key].action === "clear" ? "" : "***233",
              },
            ]),
          ) as typeof config.credentials,
        };
      }
      data = config;
    } else if (path.endsWith("/certificate/info"))
      data = {
        ...info,
        auth_code_ready:
          inboxReady && !options.wrongAttempt && !info.auth_code_used,
      };
    else if (path.endsWith("/certificate/submit")) {
      writes.push({ path, body: req.postDataJSON() });
      info = { ...info, attempt_id: "a".repeat(32), status: "ready" };
      data = info;
    } else if (path.endsWith("/certificate/geturl")) {
      writes.push({ path, body: req.postDataJSON() });
      info = {
        ...info,
        status: "pending",
        url: "https://openapi.alipay.com/gateway.do?certify_id=synthetic233",
        scan: "alipay",
      };
      if (options.unknownInit) return route.abort("failed");
      data = info;
    } else if (path.endsWith("/certificate/query")) {
      inboxReady = false;
      writes.push({ path, body: req.postDataJSON() });
      info = {
        ...info,
        cert: 1,
        status: "passed",
        url: "",
        certname: "测**",
        certno: "110***********002X",
        certtime: "2026-11-02T00:00:00Z",
        certmethod_text: "支付宝身份验证",
        certtype_text: "个人认证",
      };
      data = info;
    } else if (path.endsWith("/certificate/reset")) {
      writes.push({ path, body: req.postDataJSON() });
      info = { ...info, status: "cancelled", url: "" };
      data = info;
    } else if (path.endsWith("/account/ocr")) {
      writes.push({ path, body: req.postData() });
      if (options.unknownOCR) return route.abort("failed");
      data = { fields: { name: "测试人员", id_no: "11010519491231002X" } };
    } else if (path.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ code: 0, message: "", data }),
    });
  });
  await page.goto(
    area === "admin"
      ? "/admin/system/settings?tab=certificate"
      : options.callback
        ? "/callbacks/certification233/return" + options.callback
        : "/merchant/account/certificate",
  );
  if (!options.unauthenticated)
    await expect(
      page.getByRole("region", {
        name: area === "admin" ? "实名认证配置" : "实名认证",
        exact: true,
      }),
    ).toBeVisible();
  return {
    writes,
    documents,
    setInfo: (value: ReturnType<typeof kycInfo233>) => {
      info = value;
    },
  };
}
async function choose(page: Page, label: string, value: string) {
  await page.getByRole("button", { name: label, exact: true }).click();
  await page.getByRole("option", { name: value, exact: true }).click();
}
async function identity(page: Page) {
  await page.getByLabel("真实姓名", { exact: true }).fill("测试人员");
  await page
    .getByLabel("身份证号码", { exact: true })
    .fill("11010519491231002X");
  await page.getByLabel("同意实名认证及成功扣费", { exact: true }).check();
  await page.getByRole("button", { name: "提交认证资料", exact: true }).click();
  await page
    .getByRole("dialog", { name: "确认提交认证资料", exact: true })
    .getByRole("button", { name: "确认提交", exact: true })
    .click();
}
async function initialize(page: Page) {
  await page.getByRole("button", { name: "开始身份核验", exact: true }).click();
  await page
    .getByRole("dialog", { name: "开始身份核验", exact: true })
    .getByRole("button", { name: "开始核验", exact: true })
    .click();
}

test("admin conditional credentials, company and fee fields preserve the complete shared configuration", async ({
  page,
}) => {
  const { writes } = await fixture(page, "admin");
  await expect(
    page
      .locator("#settings-panel-certificate")
      .getByText("该分类当前没有可在线维护的设置项。", { exact: true }),
  ).toHaveCount(0);
  await choose(page, "是否开启实名认证", "微信扫码实名认证");
  for (const [label, value] of [
    ["腾讯云SecretId", "synthetic-id233"],
    ["腾讯云SecretKey", "synthetic-key233"],
    ["腾讯云RuleId", "synthetic-rule233"],
  ]) {
    await page
      .getByRole("group", { name: `${label}配置`, exact: true })
      .getByRole("button", { name: /^(设置|修改)/ })
      .click();
    await page.getByLabel(label, { exact: true }).fill(value);
  }
  await page.getByLabel("实名认证费用", { exact: true }).fill("0.10");
  await page
    .getByRole("button", { name: "保存实名认证配置", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "确认保存实名认证配置", exact: true })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(page.getByText("配置已保存。", { exact: true })).toBeVisible();
  expect(writes).toHaveLength(1);
  expect(writes[0].body).toMatchObject({
    expected_revision: "0",
    cert_open: 4,
    cert_money: "0.10",
    ocr_type: "",
    credentials: {
      cert_qcloudruleid: { action: "replace", value: "synthetic-rule233" },
    },
  });
  await expect(page.getByLabel("腾讯云SecretKey", { exact: true })).toHaveValue(
    "",
  );
  await page.getByRole("tab", { name: "OCR配置", exact: true }).click();
  await expect(
    page
      .locator("#settings-panel-ocr")
      .getByText("该分类当前没有可在线维护的设置项。", { exact: true }),
  ).toHaveCount(0);
  await choose(page, "文字识别接口", "百度云OCR");
  for (const label of ["百度云API Key", "百度云Secret Key"]) {
    await page
      .getByRole("group", { name: `${label}配置`, exact: true })
      .getByRole("button", { name: /^(设置|修改)/ })
      .click();
    await page.getByLabel(label, { exact: true }).fill("synthetic233");
  }
  await page.getByRole("button", { name: "保存OCR配置", exact: true }).click();
  await page
    .getByRole("dialog", { name: "确认保存OCR文字识别配置", exact: true })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  expect(writes).toHaveLength(2);
  expect(writes[1].body).toMatchObject({
    expected_revision: "1",
    cert_open: 4,
    cert_money: "0.10",
    ocr_type: "baidu",
  });
  await expect(
    page
      .getByRole("region", { name: "OCR文字识别配置", exact: true })
      .getByText("配置已保存。", { exact: true }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "实名认证", exact: true }).click();
  await choose(page, "是否开启实名认证", "关闭");
  await page
    .getByRole("button", { name: "管理其它服务凭据项", exact: true })
    .click();
  await page
    .getByRole("button", { name: "清除腾讯云SecretKey", exact: true })
    .click();
  await page
    .getByRole("button", { name: "保存实名认证配置", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "确认保存实名认证配置", exact: true })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(page.getByText("配置已保存。", { exact: true })).toBeVisible();
  expect(writes[2].body).toMatchObject({
    expected_revision: "2",
    cert_open: 0,
    ocr_type: "baidu",
    credentials: { cert_qcloudkey: { action: "clear", value: "" } },
  });
  await page.getByRole("tab", { name: "OCR配置", exact: true }).click();
  await choose(page, "文字识别接口", "关闭");
  await page
    .getByRole("button", { name: "管理其它服务凭据项", exact: true })
    .click();
  await page
    .getByRole("button", { name: "清除百度云API Key", exact: true })
    .click();
  await page
    .getByRole("button", { name: "清除百度云Secret Key", exact: true })
    .click();
  await page.getByRole("button", { name: "保存OCR配置", exact: true }).click();
  await page
    .getByRole("dialog", { name: "确认保存OCR文字识别配置", exact: true })
    .getByRole("button", { name: "确认保存", exact: true })
    .click();
  await expect(page.getByText("配置已保存。", { exact: true })).toBeVisible();
  expect(writes).toHaveLength(4);
  expect(writes[3].body).toMatchObject({
    expected_revision: "3",
    cert_open: 0,
    ocr_type: "",
  });
});

test("a cached credential draft survives tab changes and requires explicit discard before shared configuration reload", async ({
  page,
}) => {
  const { writes } = await fixture(page, "admin");
  await choose(page, "是否开启实名认证", "手机号三要素实名认证");
  await page
    .getByRole("button", { name: "设置手机号三要素AppCode", exact: true })
    .click();
  await page
    .getByLabel("手机号三要素AppCode", { exact: true })
    .fill("synthetic-retained-draft233");
  await page.getByRole("tab", { name: "OCR配置", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "OCR文字识别配置", exact: true }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "实名认证", exact: true }).click();
  await expect(page.getByText(/草稿已保留/)).toBeVisible();
  await expect(
    page.getByLabel("手机号三要素AppCode", { exact: true }),
  ).toHaveValue("synthetic-retained-draft233");
  await expect(
    page.getByRole("button", { name: "保存实名认证配置", exact: true }),
  ).toBeDisabled();
  const panel = page.getByRole("region", { name: "实名认证配置", exact: true });
  await expect(
    panel.getByRole("button", { name: "重新读取配置", exact: true }),
  ).toHaveCount(0);
  await pageReload248(page);
  await expect(page.getByText(/草稿已保留/)).toHaveCount(0);
  await expect(
    page.getByLabel("手机号三要素AppCode", { exact: true }),
  ).toHaveCount(0);
  expect(writes).toHaveLength(0);
});

test("merchant explicitly submits, initializes and queries certification with no hidden provider calls", async ({
  page,
}) => {
  const { writes } = await fixture(page, "merchant");
  await expect(
    page.getByRole("button", { name: "提交认证资料", exact: true }),
  ).toBeDisabled();
  await identity(page);
  expect(writes).toHaveLength(1);
  await initialize(page);
  await expect(
    page.getByRole("img", { name: "本次身份认证二维码", exact: true }),
  ).toBeVisible();
  const link = page.getByRole("link", { name: "打开认证页面", exact: true });
  await expect(link).toHaveAttribute("rel", "noopener noreferrer");
  expect(writes).toHaveLength(2);
  await page.getByRole("button", { name: "查询认证结果", exact: true }).click();
  await expect(page.getByText("已实名认证", { exact: true })).toBeVisible();
  await expect(
    page.getByText("110***********002X", { exact: true }),
  ).toBeVisible();
  expect(writes).toHaveLength(3);
  expect(writes.map((x) => x.path.split("/").at(-1))).toEqual([
    "submit",
    "geturl",
    "query",
  ]);
});

test("unknown initialization cannot be repeated and is recovered by explicit status read", async ({
  page,
}) => {
  const { writes } = await fixture(page, "merchant", { unknownInit: true });
  await identity(page);
  await initialize(page);
  await expect(page.getByRole("alert")).toContainText("不会自动重试");
  await expect(
    page.getByRole("button", { name: "开始身份核验", exact: true }),
  ).toBeDisabled();
  expect(writes).toHaveLength(2);
  await page
    .getByRole("button", { name: "读取最新认证状态", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: "打开认证页面", exact: true }),
  ).toBeVisible();
  expect(writes).toHaveLength(2);
});

test("merchant reset requires confirmation and reopens an empty form", async ({
  page,
}) => {
  const { writes } = await fixture(page, "merchant");
  await identity(page);
  await page
    .getByRole("button", { name: "重新填写认证资料", exact: true })
    .click();
  expect(writes).toHaveLength(1);
  await page
    .getByRole("dialog", { name: "重新填写认证资料", exact: true })
    .getByRole("button", { name: "取消本次并重填", exact: true })
    .click();
  await expect(page.getByLabel("真实姓名", { exact: true })).toHaveValue("");
  expect(writes.at(-1)?.path).toMatch(/\/reset$/);
});

test("OCR uploads only after consent and explicit confirmation, then requires result review", async ({
  page,
}) => {
  const { writes } = await fixture(page, "merchant"),
    ocr = page.getByRole("region", { name: "证照OCR识别", exact: true });
  await ocr.getByLabel("证照图片", { exact: true }).setInputFiles({
    name: "synthetic233.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAQAAAC1+jfqAAAAE0lEQVR42mP8z8AARMAgYBgG0QEAWnABHb5FfnAAAAAASUVORK5CYII=",
      "base64",
    ),
  });
  expect(writes).toHaveLength(0);
  await ocr
    .getByLabel("同意将所选证照发送至OCR服务商识别", { exact: true })
    .check();
  await ocr.getByRole("button", { name: "识别证照", exact: true }).click();
  expect(writes).toHaveLength(0);
  await page
    .getByRole("dialog", { name: "确认识别证照", exact: true })
    .getByRole("button", { name: "确认识别", exact: true })
    .click();
  await expect(
    ocr.getByText("识别结果，请核对", { exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("真实姓名", { exact: true })).toHaveValue("");
  await ocr
    .getByRole("button", { name: "已核对，预填认证表单", exact: true })
    .click();
  await expect(page.getByLabel("真实姓名", { exact: true })).toHaveValue(
    "测试人员",
  );
  expect(writes).toHaveLength(1);
  expect(writes[0].path).toMatch(/\/account\/ocr$/);
  await expect(
    page.getByLabel("同意实名认证及成功扣费", { exact: true }),
  ).not.toBeChecked();
});

test("corporate fields and six document types remain usable on a narrow screen", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const { writes } = await fixture(page, "merchant", { corporate: true });
  await page.getByRole("button", { name: "证件类型", exact: true }).click();
  await expect(page.getByRole("option")).toHaveCount(6);
  await page.getByRole("option", { name: "港澳通行证", exact: true }).click();
  await expect(page.getByLabel("证件号码", { exact: true })).toBeVisible();
  await choose(page, "认证类型", "企业认证");
  await expect(
    page.getByLabel("统一社会信用代码", { exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("法人姓名", { exact: true })).toBeVisible();
  await expect(page.getByLabel("身份证号码", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "证件类型", exact: true }),
  ).toHaveCount(0);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1,
  );
  expect(overflow).toBe(false);
  expect(writes).toHaveLength(0);
});

test("administrator delegated merchant sessions cannot expose identity or upload controls", async ({
  page,
}) => {
  const { writes } = await fixture(page, "merchant", { delegated: true });
  await expect(
    page.getByText("此功能需要商户本人登录，请使用商户账号登录后操作。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByLabel("真实姓名", { exact: true })).toHaveCount(0);
  expect(writes).toHaveLength(0);
});

test("server OAuth inbox redirects to a clean certification page and never auto queries", async ({
  page,
}) => {
  const id = "a".repeat(32),
    state = "s".repeat(32),
    { writes } = await fixture(page, "merchant", {
      callback: `?attempt_id=${id}&state=${state}&auth_code=syntheticcode233`,
    });
  await expect(page).toHaveURL(/\/merchant\/account\/certificate$/);
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "查询认证结果", exact: true }).click();
  expect(writes).toHaveLength(1);
  expect(writes[0].body).toEqual({
    attempt_id: id,
    state: "",
    auth_code: "",
  });
});

for (const wrongAttempt of [false, true])
  test(`expired merchant OAuth return survives login without storing or linking codes (wrong attempt ${wrongAttempt})`, async ({
    page,
  }) => {
    const id = "a".repeat(32),
      state = "s".repeat(32),
      code = "syntheticcode233";
    const { writes, documents } = await fixture(page, "merchant", {
      unauthenticated: true,
      wrongAttempt,
      callback: `?attempt_id=${id}&state=${state}&auth_code=${code}`,
    });
    await expect(page).toHaveURL(/\/merchant\/login\?return=/);
    const url = new URL(page.url());
    expect(url.searchParams.get("return")).toBe(
      "/merchant/account/certificate",
    );
    expect(page.url()).not.toContain(code);
    await page.locator('[name="username"]').fill("syntheticmerchant233");
    await page.locator('[name="password"]').fill("synthetic-password233");
    await page
      .locator("form")
      .getByRole("button", { name: "登录工作台", exact: true })
      .click();
    await expect(
      page.getByRole("region", { name: "实名认证", exact: true }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/merchant\/account\/certificate$/);
    expect(writes).toHaveLength(1);
    if (wrongAttempt)
      await expect(
        page.getByRole("button", { name: "查询认证结果", exact: true }),
      ).toBeDisabled();
    else {
      await page
        .getByRole("button", { name: "查询认证结果", exact: true })
        .click();
      await expect(page.getByText("已实名认证", { exact: true })).toBeVisible();
      expect(writes[1].body).toEqual({
        attempt_id: id,
        state: "",
        auth_code: "",
      });
    }
    const stored = await page.evaluate(() =>
      [localStorage, sessionStorage].flatMap((store) =>
        Object.keys(store).map((key) => store.getItem(key)),
      ),
    );
    expect(JSON.stringify(stored)).not.toContain(code);
    expect(documents).toContain("/merchant/login");
    expect(
      documents.filter((path) => path === "/merchant/account/certificate")
        .length,
    ).toBeGreaterThanOrEqual(2);
  });

test("provider-verified certification resumes fee completion without asking for a new identity submission", async ({
  page,
}) => {
  const { writes, setInfo } = await fixture(page, "merchant");
  setInfo(
    kycInfo233({
      cert_open: 3,
      attempt_id: "a".repeat(32),
      status: "verified",
      cert_money: "1.00",
      configuration_changed: true,
    }),
  );
  await page
    .getByRole("button", { name: "读取最新认证状态", exact: true })
    .click();
  await expect(page.getByText(/已通过服务商核验/)).toBeVisible();
  await expect(page.getByLabel("真实姓名", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "完成认证", exact: true }).click();
  await expect(page.getByText("已实名认证", { exact: true })).toBeVisible();
  expect(writes).toEqual([
    {
      path: "/console/v1/merchant/account/certificate/query",
      body: { attempt_id: "a".repeat(32), state: "", auth_code: "" },
    },
  ]);
});
