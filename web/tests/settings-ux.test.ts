import { pageReload248 } from "./helpers/page-reload248";
// @vitest-environment jsdom
import { beforeAll, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);
async function fixture(
  config: {
    readOnly?: boolean;
    extraSchemaKeys?: string[];
    extraRows?: { id: string; key: string; value: string }[];
  } = {},
) {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  const requests: string[] = [],
    state = { failRead: false, writeFailureAt: 0, malformedWriteAt: 0 };
  const rows = [
    ...(config.extraRows || []),
    { id: "1", key: "site_name", value: "本地支付" },
    { id: "2", key: "settlement_minimum_cents", value: "123" },
    { id: "3", key: "settlement_fee_rate_bps", value: "101" },
  ];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      const u = new URL(url, "http://localhost"),
        p = u.pathname;
      requests.push(p + u.search);
      let data: unknown = {};
      if (state.failRead && p.includes("/resources/settings"))
        return new Response(
          JSON.stringify({ code: -1, message: "settings unavailable" }),
          { status: 503 },
        );
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", name: "Admin", role: "admin", admin_class: "super" },
          csrf_token: "csrf",
        };
      else if (p.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (p.endsWith("/settings/schema"))
        data = {
          title: "系统设置",
          permissions: { create: true, update: true, delete: true },
          fields: [
            {
              name: "key",
              type: "select",
              label: "设置项",
              options: [
                ...(config.extraSchemaKeys || []),
                "site_name",
                "support_email",
                "registration_open",
                "settlement_minimum_cents",
                "settlement_fee_rate_bps",
                "msgconfig_domain",
                "msgconfig_risk",
              ].map((value) => ({ value, label: value })),
            },
            { name: "value", type: "string", label: "设置值", required: true },
          ],
        };
      else if (p.endsWith("/settings")) {
        const page = Number(u.searchParams.get("page") || 1);
        data = {
          items: rows.slice((page - 1) * 100, page * 100),
          total: rows.length,
        };
      } else {
        data = rows.find((x) => p.endsWith("/" + x.id)) || {};
      }
      if (config.readOnly && p.endsWith("/settings/schema"))
        (data as { permissions: unknown }).permissions = {
          create: false,
          update: false,
          delete: false,
        };
      if (options?.method && options.method !== "GET") {
        const body = JSON.parse(String(options.body));
        writes.push({ path: p, body });
        if (state.writeFailureAt === writes.length)
          return new Response(
            JSON.stringify({ code: -1, message: "storage unavailable" }),
            { status: 500 },
          );
        const row = rows.find((x) => p.endsWith("/" + x.id));
        if (row) {
          Object.assign(row, body);
          data = row;
        } else {
          const created = { id: String(9000 + writes.length), ...body };
          rows.push(created as { id: string; key: string; value: string });
          data = created;
        }
        if (state.malformedWriteAt === writes.length)
          return new Response("<html>broken response</html>", { status: 200 });
      }
      return new Response(JSON.stringify({ code: 0, data }));
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  await router.push("/admin/system/settings");
  await router.isReady();
  let w = mount(App, {
    attachTo: document.body,
    global: { plugins: [router] },
  });
  await flushPromises();
  return { w, writes, requests, state };
}
it("does not present an absent switch value as an explicitly disabled feature", async () => {
  let { w, writes } = await fixture();
  try {
    await w.get("#settings-tab-merchant").trigger("click");
    await flushPromises();
    expect(
      w.find('input[role="switch"][aria-label="开放商户注册"]').exists(),
    ).toBe(false);
    expect(w.get('button[aria-label="开放商户注册"]').text()).toBe("请选择");
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("identifies deployment-controlled IP, backend path and cache fields without editable placeholders", async () => {
  let { w, writes } = await fixture();
  try {
    await w.get("#settings-tab-iptype").trigger("click");
    await flushPromises();
    expect(
      w.get('input[aria-label="用户IP地址获取方式"]').attributes("readonly"),
    ).toBeDefined();
    expect(w.text()).toContain("X-Real-IP");
    await w.get("#settings-tab-advanced").trigger("click");
    await flushPromises();
    expect(
      w.get('input[aria-label="后台路径"]').attributes("readonly"),
    ).toBeDefined();
    expect(
      w.get('input[aria-label="缓存驱动"]').attributes("readonly"),
    ).toBeDefined();
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("restores original settings groups and saves only explicitly edited values", async () => {
  let { w, writes } = await fixture();
  try {
    expect(w.findAll('.settings-tabs [role="tab"]')).toHaveLength(17);
    expect(w.get('input[aria-label="网站名称"]').element).toHaveProperty(
      "value",
      "本地支付",
    );
    expect(w.find("table").exists()).toBe(false);
    expect(writes).toHaveLength(0);
    await w.get('input[aria-label="网站名称"]').setValue("新的本地名称");
    await w
      .findAll("button")
      .find((b) => b.text() === "保存本页设置")!
      .trigger("click");
    await flushPromises();
    expect(writes).toEqual([
      {
        path: "/console/v1/admin/resources/settings/1",
        body: { value: "新的本地名称" },
      },
    ]);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});

it("guards a draft before page reload and offers no stale save when the fresh read fails", async () => {
  let { w, writes, state } = await fixture();
  try {
    await w.get('input[aria-label="网站名称"]').setValue("尚未保存");
    const unload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(unload);
    expect(unload.defaultPrevented).toBe(true);
    state.failRead = true;
    w = await pageReload248(w);
    await flushPromises();
    const confirmation = document.querySelector(
      '[role="dialog"][aria-label="放弃未保存的设置？"]',
    );
    expect(confirmation).toBeNull();
    expect(w.text()).toContain("settings unavailable");
    expect(w.findAll("button").some((b) => b.text() === "保存本页设置")).toBe(
      false,
    );
    expect(w.find(".settings-page form").exists()).toBe(false);
    await flushPromises();
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("respects read-only schema permissions before exposing editable controls", async () => {
  let { w, writes } = await fixture({ readOnly: true });
  try {
    expect(
      w.get('input[aria-label="网站名称"]').attributes("disabled"),
    ).toBeDefined();
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("reads all pages while leaving historical unsupported keys untouched", async () => {
  const extra = Array.from({ length: 100 }, (_, i) => ({
    id: String(1000 + i),
    key: `historical_${i}`,
    value: "legacy",
  }));
  extra.push({
    id: "2000",
    key: "support_email",
    value: "support@example.invalid",
  });
  let { w, writes, requests } = await fixture({ extraRows: extra });
  try {
    expect(requests).toContain(
      "/console/v1/admin/resources/settings?page=2&limit=100",
    );
    expect(w.get('input[aria-label="客服邮箱"]').element).toHaveProperty(
      "value",
      "support@example.invalid",
    );
    expect(w.text()).not.toContain("historical_99");
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("reports partial saves accurately and never automatically retries an unknown write", async () => {
  let { w, writes, state } = await fixture();
  try {
    await w
      .findAll('.settings-tabs [role="tab"]')
      .find((x) => x.text().includes("结算规则"))!
      .trigger("click");
    await w.get('input[aria-label="最低结算金额（元）"]').setValue("1.24");
    await w.get('input[aria-label="结算手续费率（%）"]').setValue("1.02");
    state.writeFailureAt = 2;
    await w.get(".settings-page form").trigger("submit");
    await flushPromises();
    expect(w.text()).toContain("已保存1项，其余设置未完成");
    expect(w.text()).toContain("storage unavailable");
    await w.get(".settings-page form").trigger("submit");
    await flushPromises();
    expect(writes).toHaveLength(2);
    expect(writes.map((x) => x.body)).toEqual([
      { value: "124" },
      { value: "102" },
    ]);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("uses exact yuan and percent controls without treating basis points as percentages", async () => {
  let { w, writes } = await fixture();
  try {
    const tab = w
      .findAll('.settings-tabs [role="tab"]')
      .find((x) => x.text().includes("结算规则"));
    expect(tab).toBeDefined();
    await tab!.trigger("click");
    expect(
      w.get('input[aria-label="最低结算金额（元）"]').element,
    ).toHaveProperty("value", "1.23");
    expect(
      w.get('input[aria-label="结算手续费率（%）"]').element,
    ).toHaveProperty("value", "1.01");
    await w.get('input[aria-label="最低结算金额（元）"]').setValue("1.24");
    await w
      .get('#settings-panel-settle button[type="submit"][form]')
      .trigger("click");
    await flushPromises();
    expect(writes).toEqual([
      {
        path: "/console/v1/admin/resources/settings/2",
        body: { value: "124" },
      },
    ]);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});

it("shows active compatible values even when canonical keys are present only in schema", async () => {
  let { w, writes } = await fixture({
    extraSchemaKeys: ["sitename", "email", "mail_recv", "mail_name"],
    extraRows: [
      { id: "2300", key: "support_email", value: "legacy@example.invalid" },
      { id: "2301", key: "mail_name", value: "admin@example.invalid" },
    ],
  });
  try {
    expect(w.get('input[aria-label="网站名称"]').element).toHaveProperty(
      "value",
      "本地支付",
    );
    expect(w.get('input[aria-label="客服邮箱"]').element).toHaveProperty(
      "value",
      "legacy@example.invalid",
    );
    await w
      .findAll('[role="tab"]')
      .find((t) => t.text().includes("消息提醒"))!
      .trigger("click");
    await flushPromises();
    expect(
      w.get('input[aria-label="管理员接收邮箱（兼容回退）"]').element,
    ).toHaveProperty("value", "admin@example.invalid");
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});

it("preserves an explicitly cleared canonical email instead of showing a shadowed legacy value", async () => {
  let { w, writes } = await fixture({
    extraSchemaKeys: ["email"],
    extraRows: [
      { id: "2400", key: "email", value: "" },
      { id: "2401", key: "support_email", value: "shadowed@example.invalid" },
    ],
  });
  try {
    expect(w.get('input[aria-label="联系邮箱"]').element).toHaveProperty(
      "value",
      "",
    );
    expect(w.findAll('input[aria-label="联系邮箱"]')).toHaveLength(1);
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});

it("clears an active legacy contact by explicitly overriding canonical email", async () => {
  let { w, writes } = await fixture({
    extraSchemaKeys: ["email"],
    extraRows: [
      { id: "2501", key: "support_email", value: "legacy@example.invalid" },
    ],
  });
  try {
    await w.get('input[aria-label="客服邮箱"]').setValue("");
    await w
      .findAll("button")
      .find((b) => b.text() === "保存本页设置")!
      .trigger("click");
    await flushPromises();
    expect(writes).toEqual([
      {
        path: "/console/v1/admin/resources/settings",
        body: { key: "email", value: "" },
      },
    ]);
    expect(w.get('input[aria-label="联系邮箱"]').element).toHaveProperty(
      "value",
      "",
    );
    expect(w.text()).toContain("本页没有未保存修改");
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("locks generic settings after a committed but malformed success response", async () => {
  let { w, writes, state } = await fixture();
  try {
    state.malformedWriteAt = 1;
    await w.get('input[aria-label="网站名称"]').setValue("Committed New Name");
    await w
      .findAll("button")
      .find((b) => b.text() === "保存本页设置")!
      .trigger("click");
    await flushPromises();
    expect(writes).toHaveLength(1);
    expect(w.text()).toContain("请刷新核对后再保存");
    expect(
      w
        .findAll("button")
        .find((b) => b.text() === "保存本页设置")!
        .attributes("disabled"),
    ).toBeDefined();
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
