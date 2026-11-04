import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { ApiClient, ApiError } from "../src/api";
import LoginName from "../src/components/AdminLoginNameSettings245.vue";

let view: VueWrapper | undefined;
afterEach(() => view?.unmount());
async function fixture() {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-login245";
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue({ username: "original245", auth_version: "3" });
  view = mount(LoginName, {
    attachTo: document.body,
    props: { client },
    global: { stubs: { teleport: true } },
  });
  await flushPromises();
  return { client, request, view };
}
async function edit(view: VueWrapper, name = "updated245") {
  await view.get('[aria-label="修改管理员用户名"]').trigger("click");
  await view.get('[aria-label="新用户名"]').setValue(name);
  await view
    .get('[aria-label="当前登录密码"]')
    .setValue("synthetic-password245");
}
it("cancels username editing and discards the private draft without a mutation", async () => {
  const { view, request } = await fixture();
  await edit(view);
  await view
    .get('[role="dialog"]')
    .findAll("button")
    .find((b) => b.text() === "取消")!
    .trigger("click");
  expect(request).toHaveBeenCalledTimes(1);
  expect(view.find('[role="dialog"]').exists()).toBe(false);
  await view.get('[aria-label="修改管理员用户名"]').trigger("click");
  expect(view.get('[aria-label="新用户名"]').element).toHaveProperty(
    "value",
    "original245",
  );
  expect(view.get('[aria-label="当前登录密码"]').element).toHaveProperty(
    "value",
    "",
  );
});
it("validates empty, invalid, and unchanged usernames before saving", async () => {
  const { view, request } = await fixture();
  await edit(view, "");
  for (const name of ["", "bad name", "original245"]) {
    await view.get('[aria-label="新用户名"]').setValue(name);
    await view.get("form").trigger("submit");
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    expect(view.get('[role="alert"]').text()).not.toBe("");
  }
});
it("saves once with the current identity and requests a new login only after confirmed success", async () => {
  const { view, request } = await fixture();
  await edit(view);
  request.mockResolvedValueOnce({
    username: "updated245",
    reauthenticate: true,
  });
  await view.get("form").trigger("submit");
  await flushPromises();
  expect(request).toHaveBeenLastCalledWith(
    "/account/login-name",
    expect.objectContaining({
      method: "POST",
      body: {
        username: "updated245",
        expected_username: "original245",
        current_password: "synthetic-password245",
      },
    }),
  );
  expect(view.emitted("reauthenticate")).toEqual([
    ["用户名已修改，请使用新用户名重新登录"],
  ]);
  expect(view.html()).not.toContain("synthetic-password245");
});
it("keeps the username editable after a known rejection and wipes its password", async () => {
  const { view, request } = await fixture();
  await edit(view);
  request.mockRejectedValueOnce(new ApiError("用户名已被使用", 409, -1));
  await view.get("form").trigger("submit");
  await flushPromises();
  expect(view.get('[aria-label="新用户名"]').element).toHaveProperty(
    "value",
    "updated245",
  );
  expect(view.get('[aria-label="当前登录密码"]').element).toHaveProperty(
    "value",
    "",
  );
  expect(view.emitted("reauthenticate")).toBeUndefined();
});
it("does not replay a save with an unknown network result", async () => {
  const { view, request } = await fixture();
  await edit(view);
  request.mockRejectedValueOnce(new TypeError("network unavailable"));
  await view.get("form").trigger("submit");
  await flushPromises();
  expect(view.text()).toContain("未能确认");
  expect(request).toHaveBeenCalledTimes(2);
  expect(view.find('[role="dialog"]').exists()).toBe(false);
  expect(
    view.get('[aria-label="修改管理员用户名"]').attributes("disabled"),
  ).toBeDefined();
});
it("clears drafts and suppresses late success after a CSRF context change", async () => {
  const { view, request, client } = await fixture();
  await edit(view);
  let done!: (v: unknown) => void;
  request.mockImplementationOnce(
    () =>
      new Promise((r) => {
        done = r;
      }),
  );
  await view.get("form").trigger("submit");
  client.csrfToken = "changed245";
  await view.setProps({ client });
  await flushPromises();
  done({ username: "updated245", reauthenticate: true });
  await flushPromises();
  expect(view.emitted("reauthenticate")).toBeUndefined();
  expect(view.find('[role="dialog"]').exists()).toBe(false);
});
