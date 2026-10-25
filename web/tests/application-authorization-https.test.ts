// @vitest-environment-options {"url":"https://admin.example.invalid"}
import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import Panel from "../src/components/ApplicationAuthorizationPanel.vue";

it("uses the authenticated public authorization origin while the HTTPS console is on a different host", async () => {
  expect(window.location.origin).toBe("https://admin.example.invalid");
  const p = {
    id: "21",
    revision: "1",
    issuer_app_id: "2021000000000001",
    expected_auth_app_id: "2021000000000002",
    launch_kind: "basic",
  };
  const request = vi.fn(async (path: string) =>
    path.includes("capabilities")
      ? {
          merchant_id: "1000",
          available: true,
          reason: "",
          authorization_origin: "https://pay.example.invalid",
          profiles: [p],
        }
      : {
          request: {
            request_id: "a".repeat(64),
            purpose: "alipay_app_authorization",
            merchant_id: "1000",
            profile_id: "21",
            profile_revision: "1",
            issuer_app_id: p.issuer_app_id,
            expected_auth_app_id: p.expected_auth_app_id,
            launch_kind: "basic",
            app_types: null,
            status: "pending",
            expires_at: new Date(Date.now() + 120000).toISOString(),
          },
          authorization_url: `https://pay.example.invalid/identity/application/authorize/${"b".repeat(64)}`,
        },
  );
  const w = mount(Panel, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      area: "admin",
      adminClass: "super",
      ownSession: true,
    },
  });
  try {
    await w.get('[aria-label="应用授权商户ID"]').setValue("1000");
    await w.get('[data-action="app-capabilities"]').trigger("click");
    await flushPromises();
    await w.get('[aria-label="应用授权档案"]').setValue("21");
    await w.get('[data-action="app-review"]').trigger("click");
    await w.get('[data-action="app-confirm-create"]').trigger("click");
    await flushPromises();
    expect(w.get('[data-action="app-launch"]').attributes("href")).toBe(
      `https://pay.example.invalid/identity/application/authorize/${"b".repeat(64)}`,
    );
    await w.get('[data-action="app-capabilities"]').trigger("click");
    await flushPromises();
    expect(
      request.mock.calls.filter(([path]) => path.includes("capabilities")),
    ).toHaveLength(1);
  } finally {
    w.unmount();
  }
});
