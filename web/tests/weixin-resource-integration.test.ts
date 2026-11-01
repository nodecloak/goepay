// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import WeixinAppsPage from "../src/components/WeixinAppsPage.vue";
import { ApiClient } from "../src/api";

for (const adminClass of ["super", "operator"]) {
  it(`uses the encrypted application console without generic metadata CRUD for ${adminClass}`, async () => {
    const client = new ApiClient("admin");
    client.csrfToken = "synthetic-application-parent";
    const request = vi
      .spyOn(client, "request")
      .mockRejectedValue(new Error("Generic metadata API must not run"));
    const w = mount(ResourcePage, {
      props: { client, resource: "weixin-apps", adminClass, ownSession: true },
      global: {
        stubs: {
          WeixinAppsPage: {
            props: ["client"],
            template: '<section aria-label="加密微信应用目录" />',
          },
        },
      },
    });
    await flushPromises();
    expect(w.findComponent(WeixinAppsPage).exists()).toBe(true);
    expect(request).not.toHaveBeenCalled();
    expect(w.find("#field-app_id").exists()).toBe(false);
    w.unmount();
  });
}
