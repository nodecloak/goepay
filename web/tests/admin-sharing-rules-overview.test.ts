import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { createRouter, createMemoryHistory } from "vue-router";
import { ApiClient } from "../src/api";
import AdminChannelSharingRules from "../src/components/AdminChannelSharingRules.vue";
const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((v) => v.unmount());
  vi.restoreAllMocks();
});
const rule = () => ({
  id: "51",
  merchant_id: "751",
  channel_id: "7",
  revision: "1",
  provider: "wxpay",
  enabled: true,
  minimum_amount: "0",
  methods: ["native"],
  items: [
    { receiver_id: "91", mode: "rate", rate_bps: "2000", fixed_amount: "0" },
  ],
});
const entry = () => ({
  rule: rule(),
  receivers: [
    {
      id: "91",
      account_masked: "***1234",
      name_masked: "测***",
      state: "bound",
    },
  ],
});
async function setup(
  extra?: (p: string, o: any) => unknown,
  initial = "/admin/profitsharing/receiver",
) {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, opts) => {
      const value = await extra?.(path, opts);
      if (value !== undefined) return value as never;
      return { items: [entry()], total: 1, limit: 20 } as never;
    });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div/>" } }],
  });
  await router.push(initial);
  await router.isReady();
  const view = mount(AdminChannelSharingRules, {
    props: { client },
    attachTo: document.body,
    global: { plugins: [router], stubs: { teleport: true } },
  });
  views.push(view);
  await flushPromises();
  return { view, client, request, router };
}
it("keeps an unknown toggle on its original owner/version and reconciles without another write", async () => {
  let changed = false;
  const { view, request } = await setup((path, opts) => {
    if (opts?.method === "POST") {
      opts.beforeDispatch?.();
      changed = true;
      throw Error("response lost");
    }
    if (path.startsWith("/channel-sharing/rules/"))
      return { ...rule(), id: "52", revision: "2", enabled: false };
    if (path.includes("rule-overview") && changed)
      return {
        items: [
          {
            ...entry(),
            rule: { ...rule(), id: "52", revision: "2", enabled: false },
          },
        ],
        total: 1,
        limit: 20,
      };
  });
  await view.get('input[aria-label="商户ID"]').setValue("752");
  await view
    .findAll("tbody button")
    .find((b) => b.text() === "关闭")!
    .trigger("click");
  await view.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(
    request.mock.calls.find(([, o]) => o?.method === "POST")?.[1]?.body,
  ).toEqual({ merchant_id: "751", expected: "51" });
  expect(view.get('[role="dialog"]').text()).toContain("核对原规则");
  await view.get('[role="dialog"] form').trigger("submit");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(1);
  await view
    .findAll('[role="dialog"] button')
    .find((b) => b.text() === "核对原规则")!
    .trigger("click");
  await flushPromises();
  expect(view.find('[role="dialog"]').exists()).toBe(false);
  expect(view.text()).toContain("已关闭");
  expect(
    request.mock.calls.some(
      ([p]) => p === "/channel-sharing/rules/7?merchant_id=751",
    ),
  ).toBe(true);
});
it("invalid owner links cannot become a global overview by clicking refresh", async () => {
  const { view, request } = await setup(
    undefined,
    "/admin/profitsharing/receiver?merchant_id=",
  );
  expect(request).not.toHaveBeenCalled();
  await view
    .findAll("button")
    .find((b) => b.text() === "刷新")!
    .trigger("click");
  await flushPromises();
  expect(request).not.toHaveBeenCalled();
});
it("a raw session change cannot restore old rule rows or leave a permanent loading state", async () => {
  let finish!: (v: unknown) => void;
  const pending = new Promise((resolve) => {
    finish = resolve;
  });
  const { view, client } = await setup(() => pending);
  client.csrfToken = "replacement";
  finish({ items: [entry()], total: 1, limit: 20 });
  await flushPromises();
  expect(view.find("tbody").text()).not.toContain("***1234");
  expect(view.get('[role="alert"]').text()).toContain("会话");
  expect(
    view
      .findAll("button")
      .find((b) => b.text() === "刷新")!
      .attributes("disabled"),
  ).toBeUndefined();
});
