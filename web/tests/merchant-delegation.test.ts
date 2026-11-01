import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../src/api";
import Panel from "../src/components/MerchantDelegationPanel.vue";

afterEach(() => vi.restoreAllMocks());
function setup(failure?: Error, atConsume = false) {
  const calls: string[] = [];
  vi.spyOn(ApiClient.prototype, "request").mockImplementation(async (path) => {
    calls.push(path);
    if (path === "/sso/grants") {
      if (failure && !atConsume) throw failure;
      return { grant: "g".repeat(43) } as never;
    }
    if (path === "/sso/consume") {
      if (failure) throw failure;
      return {} as never;
    }
    throw new Error("session unavailable");
  });
  const w = mount(Panel, {
    props: {
      client: new ApiClient("admin"),
      merchantId: "10002",
      name: "API merchant",
      canConfigureLogin: true,
    },
  });
  return { w, calls };
}
async function submit(w: ReturnType<typeof setup>["w"]) {
  await w
    .findAll("button")
    .find((b) => b.text() === "确认建立代登录会话")!
    .trigger("click");
  await flushPromises();
}
it("explains a missing login account as a definite refusal and opens account setup without a session read", async () => {
  const { w, calls } = setup(
    new ApiError("enabled merchant login account required", 409, -1),
  );
  await submit(w);
  expect(calls).toEqual(["/sso/grants"]);
  expect(w.text()).toContain("后台登录账号");
  expect(w.text()).not.toContain("结果尚未核实");
  expect(w.text()).not.toContain("enabled merchant");
  const button = w.findAll("button").find((b) => b.text() === "设置后台登录");
  expect(button).toBeDefined();
  await button!.trigger("click");
  expect(w.emitted("configureLogin")).toHaveLength(1);
  w.unmount();
});
it.each([401, 403, 409, 422, 429])(
  "does not read or retry a definitively refused grant (%s)",
  async (status) => {
    const { w, calls } = setup(new ApiError("refused", status, -1));
    await submit(w);
    expect(calls).toEqual(["/sso/grants"]);
    expect(w.text()).not.toContain("结果尚未核实");
    w.unmount();
  },
);
it("does not read an unrelated existing session when consumption is explicitly rejected", async () => {
  const { w, calls } = setup(
    new ApiError("delegation grant unavailable", 409, -1),
    true,
  );
  await submit(w);
  expect(calls).toEqual(["/sso/grants", "/sso/consume"]);
  expect(w.text()).not.toContain("结果尚未核实");
  w.unmount();
});
it("checks the session once after an unknown consumption outcome without repeating writes", async () => {
  const { w, calls } = setup(new TypeError("network interrupted"), true);
  await submit(w);
  expect(calls).toEqual(["/sso/grants", "/sso/consume", "/session"]);
  expect(w.text()).toContain("结果尚未核实");
  expect(w.text()).toContain("核对当前商户会话");
  w.unmount();
});
it("does not inspect a previous session when grant issuance fails before consumption", async () => {
  const { w, calls } = setup(new TypeError("network interrupted"));
  await submit(w);
  expect(calls).toEqual(["/sso/grants"]);
  expect(w.text()).not.toContain("核对当前商户会话");
  w.unmount();
});

it("keeps an acknowledged consumption uncertain when subsequent session verification fails", async () => {
  const { w, calls } = setup();
  await submit(w);
  expect(calls).toEqual([
    "/sso/grants",
    "/sso/consume",
    "/session",
    "/session",
  ]);
  expect(w.text()).toContain("结果尚未核实");
  expect(w.text()).not.toContain("在新标签打开商户后台");
  w.unmount();
});

it("does not offer account setup without edit permission", async () => {
  const { w } = setup(
    new ApiError("enabled merchant login account required", 409, -1),
  );
  await w.setProps({ canConfigureLogin: false });
  await submit(w);
  expect(w.text()).toContain("后台登录账号");
  expect(w.findAll("button").some((b) => b.text() === "设置后台登录")).toBe(
    false,
  );
  w.unmount();
});

it("clears the uncertain warning when a read-only session check confirms the target", async () => {
  const { w, calls } = setup(new TypeError("network interrupted"), true);
  await submit(w);
  vi.spyOn(ApiClient.prototype, "session").mockResolvedValue({
    user: { id: "10002", name: "Target", role: "merchant" },
    csrf_token: "c",
    delegated_by: { id: "1", name: "Admin" },
  });
  await w
    .findAll("button")
    .find((b) => b.text() === "核对当前商户会话")!
    .trigger("click");
  await flushPromises();
  expect(w.find('[role="alert"]').exists()).toBe(false);
  expect(w.text()).toContain("在新标签打开商户后台");
  expect(w.text()).not.toContain("核对当前商户会话");
  expect(calls.filter((x) => x === "/sso/consume")).toHaveLength(1);
  w.unmount();
});
