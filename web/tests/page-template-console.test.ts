import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import TemplatePage from "../src/components/TemplatePage.vue";
it("loads the typed template API for own super only and never generic template CRUD", async () => {
  const request = vi.fn((_path: string) =>
    Promise.resolve({
      type: "pay",
      revision: "0",
      selected: "default",
      items: [
        { name: "default", title: "标准", description: "本地" },
        { name: "compact", title: "紧凑", description: "本地" },
      ],
    }),
  );
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  expect(request.mock.calls[0]?.[0]).toBe("/templates?type=pay");
  w.unmount();
  for (const p of [
    { adminClass: "operator", ownSession: true },
    { adminClass: "super", ownSession: false },
  ]) {
    request.mockClear();
    const v = mount(TemplatePage, {
      props: {
        client: Object.assign(new ApiClient("admin"), { request }),
        ...p,
      },
    });
    await flushPromises();
    expect(request).not.toHaveBeenCalled();
    v.unmount();
  }
});

it("preserves per-kind fields and rejects directory/config revision races before enabling writes", async () => {
  for (const kind of ["pay"]) {
    const config: Record<string, string | boolean> = {
      brand_name: "Local",
      notice: "",
      accent_color: "#007c91",
      background_color: "#f4f8fa",
      logo_asset_id: "",
      density: "comfortable",
    };
    if (kind === "pay") config.show_order_name = false;
    const request = vi.fn(async (path: string) =>
      path.includes("?")
        ? {
            type: kind,
            revision: "4",
            selected: "default",
            items: [
              { name: "default", title: "标准", description: "本地" },
              { name: "compact", title: "紧凑", description: "本地" },
            ],
          }
        : { type: kind, name: "default", revision: "5", config, fields: [] },
    );
    const w = mount(TemplatePage, {
      props: {
        client: Object.assign(new ApiClient("admin"), { request }),
        adminClass: "super",
        ownSession: true,
      },
    });
    await flushPromises();
    expect(w.find("form").exists()).toBe(false);
    expect(w.find("[role=alert]").exists()).toBe(true);
    w.unmount();
  }
});
it("does not continue configuration reads after unmount or revive a stale page", async () => {
  let resolve!: (value: unknown) => void;
  const request = vi.fn(
    (_path: string) =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  w.unmount();
  resolve({
    type: "pay",
    revision: "0",
    selected: "default",
    items: [
      { name: "default", title: "标准", description: "本地" },
      { name: "compact", title: "紧凑", description: "本地" },
    ],
  });
  await flushPromises();
  expect(request).toHaveBeenCalledTimes(1);
});


it("shows each template's preview picture on its card", async () => {
  const request = vi.fn(async (path: string) =>
    path.includes("?")
      ? {
          type: "pay",
          revision: "0",
          selected: "default",
          items: [
            { name: "default", title: "默认支付页模板", description: "本地" },
            { name: "compact", title: "紧凑支付页模板", description: "本地" },
          ],
        }
      : {
          type: "pay",
          name: "default",
          revision: "0",
          config: {
            brand_name: "Local",
            notice: "",
            accent_color: "#007c91",
            background_color: "#f4f8fa",
            logo_asset_id: "",
            density: "comfortable",
            show_order_name: false,
          },
          fields: [],
        },
  );
  const w = mount(TemplatePage, {
    props: {
      client: Object.assign(new ApiClient("admin"), { request }),
      adminClass: "super",
      ownSession: true,
    },
  });
  await flushPromises();
  expect(
    w.findAll("img.template-thumb").map((i) => i.attributes("src")),
  ).toEqual([
    "/static/template-previews/pay-default.jpg",
    "/static/template-previews/pay-compact.jpg",
  ]);
  // The card's 预览 button opens the template over the main content area.
  await w.get('button[aria-label="预览 紧凑支付页模板"]').trigger("click");
  const pane = w.get(".template-fullpreview");
  expect(pane.attributes("role")).toBe("dialog");
  expect(pane.get("iframe").attributes("src")).toBe(
    "/console/v1/admin/templates/pay/compact/preview",
  );
  expect(pane.text()).toContain("紧凑支付页模板");
  await pane.get('button[aria-label="关闭预览"]').trigger("click");
  expect(w.find(".template-fullpreview").exists()).toBe(false);
  // Esc closes it too, and opening a preview does not change the selection.
  await w.get('button[aria-label="预览 默认支付页模板"]').trigger("click");
  await w.get(".template-fullpreview").trigger("keydown", { key: "Escape" });
  expect(w.find(".template-fullpreview").exists()).toBe(false);
  expect(request.mock.calls.every((c) => !String(c[0]).includes("/select"))).toBe(
    true,
  );
  w.unmount();
});
