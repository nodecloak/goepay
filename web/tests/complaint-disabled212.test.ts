import { expect, it } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import ComplaintsPage from "../src/components/ComplaintsPage.vue";
import { ApiClient, ApiError } from "../src/api";

// Break caught: a closed feature is rendered as a failed, still-operable form,
// or an unrelated permission/server error is incorrectly classified as closed.
async function render(path: string, failure: Error, canConfigure = false) {
  const client = new ApiClient("admin");
  client.request = async () => {
    throw failure;
  };
  const wrapper = mount(ComplaintsPage, {
    props: { client, area: "admin", path, canConfigure },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  return wrapper;
}

for (const path of ["/complain/list", "/complain/charts", "/complain/task"])
  it(`closed ${path} explains availability and removes invalid business actions`, async () => {
    const w = await render(
      path,
      new ApiError("complaint feature disabled", 403, -1),
      true,
    );
    expect(w.find('[role="alert"]').exists()).toBe(false);
    expect(w.text()).toContain("交易投诉尚未启用");
    expect(w.find("table").exists()).toBe(false);
    expect(w.find('[data-action="batch-delete"]').exists()).toBe(false);
    expect(w.find('[aria-label="搜索内容"]').exists()).toBe(false);
    expect(
      w.get('a[href="/admin/system/settings?tab=complaint"]').text(),
    ).toContain("前往投诉设置");
    w.unmount();
  });

it("a closed complaint feature never offers a settings link without explicit permission", async () => {
  const w = await render(
    "/complain/list",
    new ApiError("complaint feature disabled", 403, -1),
  );
  expect(w.text()).toContain("请联系平台管理员");
  expect(w.find('a[href*="system/settings"]').exists()).toBe(false);
  w.unmount();
});

for (const [status, code, message] of [
  [403, -1, "administrator required"],
  [503, -1, "complaint feature unavailable"],
  [500, -1, "complaint feature disabled"],
  [403, 123, "complaint feature disabled"],
] as const)
  it(`keeps ${status}/${code}/${message} separate from the feature-disabled state`, async () => {
    const w = await render(
      "/complain/list",
      new ApiError(message, status, code),
    );
    expect(w.get('[role="alert"]').text()).toContain(
      new ApiError(message, status, code).message,
    );
    expect(w.text()).not.toContain("交易投诉尚未启用");
    w.unmount();
  });

it("a successful availability retry restores the real complaint controls", async () => {
  let disabled = true;
  const client = new ApiClient("admin");
  client.request = async (path: string) => {
    if (disabled) throw new ApiError("complaint feature disabled", 403, -1);
    return (
      path.startsWith("/complaints133")
        ? { items: [], total: 0 }
        : { items: [], summary: {} }
    ) as never;
  };
  const w = mount(ComplaintsPage, {
    props: { client, area: "admin", path: "/complain/list" },
  });
  await flushPromises();
  disabled = false;
  const retry = w
    .findAll("button")
    .find((b) => b.text() === "重新检查功能状态");
  expect(retry).toBeDefined();
  await retry!.trigger("click");
  await flushPromises();
  expect(w.text()).not.toContain("交易投诉尚未启用");
  expect(w.find('[aria-label="搜索内容"]').exists()).toBe(true);
  w.unmount();
});
