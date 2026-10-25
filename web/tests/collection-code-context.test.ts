import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import CollectionCodePanel from "../src/components/CollectionCodePanel.vue";
import type { ApiClient } from "../src/api";
vi.mock("qrcode", () => ({
  default: { toDataURL: vi.fn(async () => "data:image/png;base64,local") },
}));
it("never shows an earlier collection token after the selected record changes", async () => {
  let resolve: (v: unknown) => void = () => {};
  const request = vi.fn(() => new Promise((r) => (resolve = r)));
  const wrapper = mount(CollectionCodePanel, {
    props: { client: { request } as unknown as ApiClient, id: "1" },
  });
  await wrapper.get("button").trigger("click");
  await wrapper.setProps({ id: "2" });
  resolve({
    url: "http://127.0.0.1:8186/collect/1?token=old",
    expires_at: "tomorrow",
  });
  await flushPromises();
  expect(wrapper.find("input").exists()).toBe(false);
  expect(wrapper.find("img").exists()).toBe(false);
  wrapper.unmount();
});
