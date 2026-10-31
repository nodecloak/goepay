// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
async function fixture(
  config: {
    readOnly?: boolean;
    extraRows?: { id: string; key: string; value: string }[];
  } = {},
) {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  const requests: string[] = [],
    state = { failRead: false, writeFailureAt: 0 };
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
        }
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
  const w = mount(App, {
    attachTo: document.body,
    global: { plugins: [router] },
  });
  await flushPromises();
  return { w, writes, requests, state };
}
it("restores original settings groups and saves only explicitly edited values", async () => {
  const { w, writes } = await fixture();
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

it("blocks writes after a failed refresh rather than saving stale unconfirmed settings", async () => {
  const { w, writes, state } = await fixture();
  try {
    await w.get('input[aria-label="网站名称"]').setValue("尚未保存");
    state.failRead = true;
    await w
      .findAll("button")
      .find((b) => b.text() === "刷新设置")!
      .trigger("click");
    await flushPromises();
    const confirmation = document.querySelector(
      '[role="dialog"][aria-label="放弃未保存的设置？"]',
    );
    expect(confirmation).not.toBeNull();
    const discard = [...confirmation!.querySelectorAll("button")].find(
      (button) => button.textContent?.trim() === "放弃修改并刷新",
    );
    expect(discard).toBeDefined();
    discard!.click();
    await flushPromises();
    expect(w.text()).toContain("settings unavailable");
    expect(
      w
        .findAll("button")
        .find((b) => b.text() === "保存本页设置")!
        .attributes("disabled"),
    ).toBeDefined();
    await w.get(".settings-page form").trigger("submit");
    await flushPromises();
    expect(writes).toHaveLength(0);
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("respects read-only schema permissions before exposing editable controls", async () => {
  const { w, writes } = await fixture({ readOnly: true });
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
  const { w, writes, requests } = await fixture({ extraRows: extra });
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
  const { w, writes, state } = await fixture();
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
  const { w, writes } = await fixture();
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
