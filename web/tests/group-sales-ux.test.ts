// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";

async function fixture(
  area: "admin" | "merchant",
  path: string,
  records: Record<string, unknown>[] = [],
) {
  const posts: { path: string; body: Record<string, unknown> }[] = [];
  const fetcher = vi.fn(async (url: string, options?: RequestInit) => {
    const u = new URL(url, "http://localhost"),
      p = u.pathname;
    let data: unknown = { items: [], total: 0 };
    if (p.endsWith("/session"))
      data = {
        user: { id: "1", name: "test", role: area, admin_class: "super" },
        csrf_token: "csrf",
      };
    else if (p.endsWith("/security/payment-password"))
      data = {
        configured: false,
        enforced: false,
        credential_epoch: 0,
        totp_required: false,
      };
    else if (p.endsWith("/workflows/memberships"))
      data = { items: records, total: records.length };
    else if (p.endsWith("/membership-sales"))
      data =
        area === "admin"
          ? {
              enabled: true,
              revision: "a".repeat(64),
              groups: [
                {
                  id: "3",
                  name: "专业组",
                  enabled: true,
                  sale_enabled: true,
                  price_cents: "100",
                  duration_months: 1,
                  duration_days: 30,
                  sort: 0,
                  visible: "",
                  description: "简介",
                },
              ],
            }
          : {
              enabled: true,
              items: [
                {
                  id: "3",
                  name: "专业组",
                  price_cents: "100",
                  duration_months: 1,
                  duration_days: 0,
                  permanent: false,
                  description: "简介",
                  sort: 0,
                },
              ],
            };
    if (options?.method && options.method !== "GET") {
      posts.push({ path: p, body: JSON.parse(String(options.body)) });
      if (p.endsWith("/membership-sales"))
        (data as { revision: string }).revision = "b".repeat(64);
    }
    return new Response(JSON.stringify({ code: 0, data }));
  });
  vi.stubGlobal("fetch", fetcher);
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  await router.push(`/${area}${path}`);
  await router.isReady();
  const w = mount(App, {
    attachTo: document.body,
    global: {
      plugins: [router],
      stubs: {
        KitModal: {
          props: ["title", "submitText"],
          emits: ["close", "submit"],
          template:
            '<div role="dialog" :aria-label="title"><slot /><button type="button" @click="$emit(\'submit\')">{{ submitText || \'保存\' }}</button></div>',
        },
      },
    },
  });
  await flushPromises();
  return { w, posts, fetcher };
}

it("shows GID and 用户组名称 as distinct columns with Pro's 有效期（月） header", async () => {
  const { w } = await fixture("admin", "/merchant/group-buy");
  try {
    const headers = w.findAll("thead th").map((x) => x.text());
    expect(headers.slice(0, 4)).toEqual([
      "GID",
      "用户组名称",
      "售价（元）",
      "有效期（月）",
    ]);
    // GID is its own cell, not folded into the name cell.
    const firstRow = w.findAll("tbody tr")[0]!;
    expect(firstRow.findAll("td")[0]!.text()).toBe("3");
    expect(firstRow.findAll("td")[1]!.text()).toContain("专业组");
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});

it("uses the admin group-buy route for sales configuration and saves exact price/month fields", async () => {
  const { w, posts } = await fixture("admin", "/merchant/group-buy");
  try {
    expect(w.text()).toContain("用户组购买开关");
    expect(w.find('input[aria-label="商户 ID"]').exists()).toBe(false);
    expect(posts).toHaveLength(0);
    await w.get('input[aria-label="售价（元） · 专业组"]').setValue("2.99");
    await w.get('input[aria-label="有效期（月） · 专业组"]').setValue("0");
    await w
      .findAll("button")
      .find((x) => x.text() === "保存全部")!
      .trigger("click");
    await flushPromises();
    expect(posts).toHaveLength(1);
    expect(posts[0].path).toBe("/console/v1/admin/membership-sales");
    expect(
      (posts[0].body.groups as Record<string, unknown>[])[0],
    ).toMatchObject({ id: "3", price_cents: "299", duration_months: 0 });
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});

it("shows a permanent purchase as permanent and does not expose the internal sentinel as a finite expiry", async () => {
  const { w } = await fixture("merchant", "/account/groupbuy", [
    {
      id: "1",
      group_name: "永久组",
      price: "5.00",
      duration_days: 0,
      period_months: 0,
      permanent: true,
      quantity: 1,
      expires_at: "9999-12-30T16:00:00Z",
      external_no: "permanent-one",
    },
  ]);
  try {
    expect(w.get("tbody").text()).not.toContain("9999");
    expect(w.get("tbody").text()).toContain("永久");
    expect(w.findAll("thead th").map((x) => x.text())).not.toContain(
      "duration_days",
    );
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});

it("uses the merchant sale catalog and confirms quantity against an exact total", async () => {
  const { w, posts, fetcher } = await fixture("merchant", "/account/groupbuy");
  try {
    expect(
      fetcher.mock.calls.some(([url]) =>
        String(url).endsWith("/membership-sales"),
      ),
    ).toBe(true);
    expect(w.text()).toContain("1个月");
    expect(posts).toHaveLength(0);
    await w
      .findAll("button")
      .find((x) => x.text() === "购买会员")!
      .trigger("click");
    await w
      .get('button.pa-select-trigger[aria-label="购买用户组"]')
      .trigger("click");
    await w
      .findAll('[role="option"]')
      .find((o) => o.text().includes("专业组"))!
      .trigger("click");
    await w.get('input[aria-label="购买数量"]').setValue("2");
    expect(w.text()).toContain("2.00 元");
    await w
      .findAll("button")
      .find((x) => x.text() === "确认购买会员")!
      .trigger("click");
    await flushPromises();
    expect(posts[0].body).toMatchObject({ group_id: "3", quantity: 2 });
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
