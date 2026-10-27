import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import { ApiClient } from "../src/api";
it("the original default group editor reads the executable oversea inheritance field", async () => {
  const c = new ApiClient("admin");
  c.csrfToken = "synthetic-risk177-group";
  const request = vi.fn(async (path: string) => {
    if (path.endsWith("/schema"))
      return {
        title: "用户组",
        fields: [{ name: "name", label: "名称", type: "string" }],
        permissions: { create: true, update: true, delete: false },
      };
    if (path === "/resources/groups/0")
      return { id: "0", name: "默认组", enabled: true };
    if (path === "/payment-risk/groups/0")
      return {
        group_id: "0",
        group_name: "默认组",
        configured: false,
        revision: "0",
        fingerprint: "a".repeat(64),
        config: { pay_block_oversea: "", check_pay_regoin: "" },
        effective_oversea: "0",
        effective_scan_region: "0",
        geo_configured: true,
      };
    throw Error("unexpected group request");
  });
  c.request = request as ApiClient["request"];
  const w = mount(ResourcePage, {
    props: {
      resource: "groups",
      client: c,
      ownSession: true,
      adminClass: "super",
      entry: {
        mode: "edit",
        id: "0",
        returnPath: "/admin/merchant/group-list",
      },
    },
    global: { stubs: { RouterLink: true } },
  });
  await flushPromises();
  await w.get('[role="tab"][data-group="other"]').trigger("click");
  await flushPromises();
  expect(w.text()).toContain("禁止海外IP支付");
  expect(
    request.mock.calls.some(([path]) => path === "/payment-risk/groups/0"),
  ).toBe(true);
  w.unmount();
});
