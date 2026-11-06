// @vitest-environment jsdom
import { beforeAll, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import App from "../src/App.vue";
import { preloadAppPages } from "./app-pages";

beforeAll(preloadAppPages);
async function fixture(
  area: "admin" | "merchant",
  path: string,
  defer42 = false,
) {
  const requests: {
    path: string;
    method: string;
    body?: Record<string, unknown>;
  }[] = [];
  let release42 = () => {};
  const pending42 = new Promise<void>((resolve) => {
    release42 = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, options?: RequestInit) => {
      const u = new URL(url, "http://localhost"),
        p = u.pathname,
        method = options?.method || "GET";
      requests.push({
        path: p + u.search,
        method,
        body: options?.body ? JSON.parse(String(options.body)) : undefined,
      });
      let data: unknown = { items: [], total: 0 };
      if (p.endsWith("/session"))
        data = {
          user: { id: "1", name: area, role: area, admin_class: "super" },
          csrf_token: "csrf",
        };
      else if (p.endsWith("/security/payment-password"))
        data = {
          configured: false,
          enforced: false,
          credential_epoch: 0,
          totp_required: false,
        };
      else if (p.endsWith("/schema"))
        data = {
          title: "进件商户资料",
          description: "本地进件草稿，不代表获得上游商户资格。",
          permissions: { create: true, update: true, delete: true },
          fields: [
            {
              name: "business_name",
              label: "经营名称",
              type: "string",
              required: true,
            },
            {
              name: "contact_name",
              label: "联系人",
              type: "string",
              required: true,
            },
            {
              name: "contact_phone",
              label: "联系电话",
              type: "string",
              required: true,
            },
            {
              name: "channel_id",
              label: "进件渠道ID",
              type: "string",
              required: true,
            },
          ],
        };
      else if (p.endsWith("/42"))
        data = {
          id: "42",
          business_name: "原经营名称",
          contact_name: "张三",
          contact_phone: "13000000000",
          channel_id: "3",
          status: "draft",
        };
      else if (p.endsWith("/43"))
        data = {
          id: "43",
          business_name: "第二份资料",
          contact_name: "李四",
          contact_phone: "13000000001",
          channel_id: "4",
          status: "draft",
        };
      if (p.endsWith("/42") && defer42) await pending42;
      if (method !== "GET")
        data = { id: "42", ...JSON.parse(String(options?.body)) };
      return new Response(JSON.stringify({ code: 0, data }));
    }),
  );
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  await router.push(`/${area}${path}`);
  await router.isReady();
  const w = mount(App, {
    attachTo: document.body,
    global: { plugins: [router] },
  });
  await flushPromises();
  return { w, requests, router, release42 };
}
it("opens the original merchant create route as a read-only initialized inline local draft form", async () => {
  const { w, requests } = await fixture("merchant", "/applyments/create");
  try {
    expect(w.find('input[id="field-business_name"]').exists()).toBe(true);
    expect(w.find('[role="dialog"]').exists()).toBe(false);
    expect(w.find("table").exists()).toBe(false);
    expect(
      requests.some(
        (r) =>
          r.path.includes("onboarding-applications?") || r.method !== "GET",
      ),
    ).toBe(false);
    expect(w.text()).toContain("本地进件草稿");
    for (const [key, value] of Object.entries({
      business_name: "新的经营名称",
      contact_name: "张三",
      contact_phone: "13000000000",
      channel_id: "3",
    }))
      await w.get(`#field-${key}`).setValue(value);
    await w.findAll("form").at(-1)!.trigger("submit");
    await flushPromises();
    expect(requests.filter((r) => r.method !== "GET")).toHaveLength(1);
    expect(w.text()).toContain("新增已保存");
    expect(w.find("#field-business_name").exists()).toBe(false);
    expect(
      w.findAll('a[href="/merchant/applyments/merchant"]').map((a) => a.text()),
    ).toContain("返回进件资料列表");
  } finally {
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it("ignores a late old record response after the original form query ID changes", async () => {
  const { w, requests, router, release42 } = await fixture(
    "merchant",
    "/applyments/form?id=42",
    true,
  );
  try {
    await router.push("/merchant/applyments/form?id=43");
    await flushPromises();
    expect(w.get("#field-business_name").element).toHaveProperty(
      "value",
      "第二份资料",
    );
    release42();
    await flushPromises();
    expect(w.get("#field-business_name").element).toHaveProperty(
      "value",
      "第二份资料",
    );
    expect(requests.every((r) => r.method === "GET")).toBe(true);
  } finally {
    release42();
    w.unmount();
    vi.unstubAllGlobals();
  }
});
it.each(["admin", "merchant"] as const)(
  "opens %s hidden form route by original record ID without listing other records",
  async (area) => {
    const { w, requests } = await fixture(area, "/applyments/form?id=42");
    try {
      expect(w.get("#field-business_name").element).toHaveProperty(
        "value",
        "原经营名称",
      );
      expect(w.find("table").exists()).toBe(false);
      expect(
        requests.some((r) => r.path.endsWith("/onboarding-applications/42")),
      ).toBe(true);
      expect(
        requests.some((r) => r.path.includes("onboarding-applications?")),
      ).toBe(false);
      expect(requests.every((r) => r.method === "GET")).toBe(true);
    } finally {
      w.unmount();
      vi.unstubAllGlobals();
    }
  },
);
it.each([
  "/applyments/form?id=42&id=43",
  "/applyments/form?id=9223372036854775808",
])(
  "rejects an invalid edit ID in %s instead of opening an unintended create form",
  async (path) => {
    const { w, requests } = await fixture("merchant", path);
    try {
      expect(w.text()).toContain("资料编号无效");
      expect(w.find("#field-business_name").exists()).toBe(false);
      expect(
        requests.some(
          (r) =>
            /onboarding-applications\/(42|43)$/.test(r.path) ||
            r.method !== "GET",
        ),
      ).toBe(false);
    } finally {
      w.unmount();
      vi.unstubAllGlobals();
    }
  },
);
