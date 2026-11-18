import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";
import ParentChannelPage from "../src/components/ParentChannelPage.vue";

const route = "/payment-channels/crypto/networks";
const tron = {
  network: "tron",
  enabled: false,
  effective_enabled: false,
  revision: "1",
  wallets_configured: true,
  deposit_address_available: true,
  listener_ready: true,
  ready_to_enable: true,
};
const ethereum = {
  ...tron,
  network: "ethereum",
  wallets_configured: false,
  deposit_address_available: false,
  listener_ready: false,
  ready_to_enable: false,
};

let w: VueWrapper | undefined;
afterEach(() => {
  w?.unmount();
  w = undefined;
});

async function openControls(
  read: () => unknown = () => ({ items: [tron, ethereum] }),
  update: () => unknown = () => ({
    ...tron,
    enabled: true,
    effective_enabled: true,
    revision: "2",
  }),
) {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) => {
      if (path === route) return read();
      if (path === route + "/tron") return update();
      if (path === "/payment-channels/templates") return { items: [] };
      if (path.startsWith("/payment-channels?"))
        return { items: [], total: 0, page: 1, size: 20 };
      throw new Error("Unexpected local route " + path);
    });
  w = mount(ParentChannelPage, {
    props: { client, allowed: true },
    global: { stubs: { teleport: true } },
  });
  await flushPromises();
  expect(request.mock.calls.some(([path]) => path === route)).toBe(false);
  await w.get("button[data-crypto-network-controls]").trigger("click");
  await flushPromises();
  return { panel: w, request };
}

function writes(request: Awaited<ReturnType<typeof openControls>>["request"]) {
  return request.mock.calls.filter(
    ([, options]) => options?.method === "PATCH",
  );
}

it("shows all prerequisite results and disables an unready chain without a write", async () => {
  const { panel, request } = await openControls();
  expect(panel.get('[role="dialog"]').attributes("aria-label")).toBe(
    "USDT 网络开关",
  );
  const eth = panel.get('[data-network="ethereum"]');
  expect(eth.text()).toContain("钱包地址");
  expect(eth.text()).toContain("收款地址池");
  expect(eth.text()).toContain("监听服务");
  expect(eth.get("button").attributes("disabled")).toBeDefined();
  expect(writes(request)).toHaveLength(0);
});

it("requires a fresh password and explicit confirmation, then refreshes the payment channels", async () => {
  const { panel, request } = await openControls();
  await panel.get('[data-network="tron"] button').trigger("click");
  const dialog = panel.get('[role="dialog"]');
  expect(
    dialog.get('button[type="submit"]').attributes("disabled"),
  ).toBeDefined();
  expect(writes(request)).toHaveLength(0);
  await dialog.get('input[type="password"]').setValue("local-only-password");
  await dialog.get("form").trigger("submit");
  await flushPromises();
  expect(writes(request)).toHaveLength(1);
  expect(writes(request)[0]).toEqual([
    route + "/tron",
    expect.objectContaining({
      method: "PATCH",
      body: {
        enabled: true,
        expected_revision: "1",
        current_password: "local-only-password",
      },
    }),
  ]);
  const updated = panel.get('[role="dialog"]');
  expect(updated.find('input[type="password"]').exists()).toBe(false);
  expect(updated.get('[data-network="tron"]').text()).toContain("已开启");
  expect(
    request.mock.calls.filter(([path]) =>
      path.startsWith("/payment-channels?"),
    ),
  ).toHaveLength(2);
});

it("never replays a lost update response and only verifies through a fresh read", async () => {
  const { panel, request } = await openControls(
    () => ({ items: [tron, ethereum] }),
    () => {
      throw new Error("response lost");
    },
  );
  await panel.get('[data-network="tron"] button').trigger("click");
  const dialog = panel.get('[role="dialog"]');
  await dialog.get('input[type="password"]').setValue("local-only-password");
  await dialog.get("form").trigger("submit");
  await flushPromises();
  const verification = panel.get('[role="dialog"]');
  expect(verification.text()).toContain("结果尚未确认");
  expect(verification.find('input[type="password"]').exists()).toBe(false);
  expect(writes(request)).toHaveLength(1);
  await verification
    .get("button[data-crypto-network-refresh]")
    .trigger("click");
  await flushPromises();
  expect(request.mock.calls.filter(([path]) => path === route)).toHaveLength(2);
  expect(
    panel.get('[data-network="tron"] button').attributes("disabled"),
  ).toBeDefined();
  expect(writes(request)).toHaveLength(1);
});
