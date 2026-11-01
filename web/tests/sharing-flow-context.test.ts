import { expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import ChannelSharingPage from "../src/components/ChannelSharingPage.vue";
it("opens the operation panel on the exact row merchant without requiring a second manual owner selection", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) =>
      path.includes("/channels")
        ? ({
            merchant_id: "9007199254740993",
            available: false,
            reason: "本地没有实时通道",
            channels: [],
          } as never)
        : ({ items: [], next_cursor: "0" } as never),
    );
  const view = mount(ChannelSharingPage, {
    props: {
      client,
      area: "admin",
      page: "order",
      initialMerchantId: "9007199254740993",
      initialExternalNo: "original-sharing",
    },
  });
  try {
    await flushPromises();
    expect(request.mock.calls[0]?.[0]).toContain(
      "merchant_id=9007199254740993",
    );
    expect(
      view.get('input[aria-label="商户 ID"]').attributes("disabled"),
    ).toBeDefined();
    expect(view.text()).toContain("original-sharing");
    expect(
      request.mock.calls.every(([, o]) => !o?.method || o.method === "GET"),
    ).toBe(true);
  } finally {
    view.unmount();
  }
});
