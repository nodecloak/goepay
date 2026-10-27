import { mount, flushPromises } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import SystemUpdatePage from "../src/components/SystemUpdatePage.vue";
import { ApiClient } from "../src/api";
import actual from "../e2e/systemupdate183-http-fixture.json";

it("rechecks raw client context at confirmation even without a reactive prop change", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "original183-raw";
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue(actual.available);
  const w = mount(SystemUpdatePage, {
    props: { client, ownSession: true, adminClass: "super" },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  await w.get('[data-action="prepare"]').trigger("click");
  await w.get("input").setValue(true);
  client.csrfToken = "replaced183-raw";
  await w.get('[data-action="confirm"]').trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(0);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(w.text()).not.toContain("合成离线版本183");
  w.unmount();
});

it("reads a lost prepare result before any further POST and preserves prepared versus applied", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "lost183";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method === "POST") throw Error("synthetic lost response");
      if (path.includes("/preparations/")) return actual.prepared as never;
      return actual.available as never;
    });
  const w = mount(SystemUpdatePage, {
    props: { client, ownSession: true, adminClass: "super" },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  await w.get('[data-action="prepare"]').trigger("click");
  await w.get('input[type="checkbox"]').setValue(true);
  await w.get('[data-action="confirm"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("提交结果尚未确认");
  await w
    .findAll("button")
    .find((b) => b.text() === "核对原更新结果")!
    .trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(1);
  expect(
    request.mock.calls.some(
      ([p]) =>
        p === "/system-update/preparations/" + actual.available.fingerprint,
    ),
  ).toBe(true);
  expect(w.text()).toContain("准备完成，等待部署");
  expect(w.findAll("h3").map((h) => h.text())).not.toContain("更新已部署");
  w.unmount();
});
it("explicit retry and withdrawal use the reviewed original state fingerprints", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "retry183";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method === "POST")
        return (
          path.endsWith("/retry") ? actual.prepared : actual.cancelled
        ) as never;
      return {
        ...actual.available,
        can_prepare: false,
        preparation: actual.unknown,
      } as never;
    });
  const w = mount(SystemUpdatePage, {
    props: { client, ownSession: true, adminClass: "super" },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  await w.get('[data-action="retry"]').trigger("click");
  await w.get("input").setValue(true);
  await w.get('[data-action="confirm"]').trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.find(([p]) => p.endsWith("/retry"))?.[1]?.body,
  ).toEqual({
    expected_fingerprint: actual.unknown.intent,
    expected_state: actual.unknown.state_fingerprint,
  });
  await w.get('[data-action="cancel"]').trigger("click");
  await w.get("input").setValue(true);
  await w.get('[data-action="confirm"]').trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.find(([p]) => p.endsWith("/cancel"))?.[1]?.body,
  ).toEqual({ expected_state: actual.prepared.state_fingerprint });
  expect(w.text()).toContain("已取消");
  w.unmount();
});

it("matches original version fields and shows changelog as literal text before confirmation", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "local183";
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue(actual.available);
  const w = mount(SystemUpdatePage, {
    props: { client, ownSession: true, adminClass: "super" },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  expect(w.text()).toContain("当前版本");
  expect(w.text()).toContain("0.1.0");
  expect(w.text()).toContain("合成离线版本183");
  expect(w.find("script").exists()).toBe(false);
  expect(w.text()).toContain("<script>literal changelog</script>");
  await w.get('[data-action="prepare"]').trigger("click");
  expect(w.get('[role="dialog"]').text()).toContain("确认更新");
  expect(request).toHaveBeenCalledTimes(1);
  expect(w.get('[data-action="confirm"]').attributes("disabled")).toBeDefined();
  w.unmount();
});
it("aborts revoked context and never accepts its late update facts", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "local183-revocation";
  let resolve!: (v: unknown) => void;
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(() => new Promise((r) => (resolve = r)) as never);
  const w = mount(SystemUpdatePage, {
    props: { client, ownSession: true, adminClass: "super" },
  });
  await w.setProps({ ownSession: false });
  resolve(actual.available);
  await flushPromises();
  expect(w.text()).not.toContain("合成离线版本183");
  expect(request).toHaveBeenCalledTimes(1);
  w.unmount();
});
