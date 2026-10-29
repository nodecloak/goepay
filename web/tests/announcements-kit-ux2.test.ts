import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import AnnouncementsPage from "../src/components/AnnouncementsPage.vue";

function makeClient(total: number) {
  const schema = {
    permissions: { create: true, update: true, delete: true },
  };
  const items = Array.from({ length: Math.min(total, 20) }, (_, i) => ({
    id: String(i + 1),
    content: "公告" + (i + 1),
    content_format: "text",
    sort: 50,
    color: "",
    enabled: true,
    created_at: "2026-10-29T00:00:00Z",
  }));
  return {
    csrfToken: "t",
    area: "admin",
    request: vi.fn().mockImplementation((path: string) => {
      if (path.endsWith("/schema")) return Promise.resolve(schema);
      return Promise.resolve({ items, total });
    }),
  } as unknown as import("../src/api").ApiClient;
}

it("announcement list shows the kit pager once there is more than one page", async () => {
  const wrapper = mount(AnnouncementsPage, {
    props: { client: makeClient(45), ownSession: true },
    global: {
      stubs: { RichTextEditor: true, RichContent: true },
    },
  });
  await flushPromises();
  expect(wrapper.find(".pa-pager").exists()).toBe(true);
});

it("announcement list shows no pager for a single page", async () => {
  const wrapper = mount(AnnouncementsPage, {
    props: { client: makeClient(3), ownSession: true },
    global: {
      stubs: { RichTextEditor: true, RichContent: true },
    },
  });
  await flushPromises();
  expect(wrapper.find(".pa-pager").exists()).toBe(false);
});
