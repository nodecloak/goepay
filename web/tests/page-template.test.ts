import { it, expect } from "vitest";
import {
  templateConfigBody,
  readTemplateConfig,
  readTemplateList,
} from "../src/page-template";
const view = {
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
    show_order_name: true,
  },
  fields: [],
};
it("uses exact typed configuration and original global type revision without arbitrary HTML or URLs", () => {
  expect(templateConfigBody(view as any, view.config)).toEqual({
    expected_revision: "0",
    config: view.config,
  });
  for (const edit of [
    { ...view.config, logo_asset_id: "https://local.invalid/x" },
    { ...view.config, template_html: "<script>x</script>" },
    { ...view.config, accent_color: "red;display:none" },
    { ...view.config, show_order_name: "true" },
  ])
    expect(() => templateConfigBody(view as any, edit as any)).toThrow();
  expect(readTemplateConfig(view).name).toBe("default");
  expect(() => readTemplateConfig({ ...view, name: "../file" })).toThrow();
});

it("requires all six common fields, adds only pay order switch, and preserves explicit false", () => {
  for (const kind of ["home", "pay", "qrpay"]) {
    const config: Record<string, string | boolean> = { ...view.config };
    delete config.show_order_name;
    if (kind === "pay") config.show_order_name = false;
    const original = { ...view, type: kind, config };
    expect(readTemplateConfig(original).config).toEqual(config);
    expect(templateConfigBody(original as any, config)).toEqual({
      expected_revision: "0",
      config,
    });
    const missing = { ...config };
    delete missing.notice;
    expect(() => templateConfigBody(original as any, missing)).toThrow();
    if (kind !== "pay")
      expect(() =>
        templateConfigBody(original as any, {
          ...config,
          show_order_name: false,
        }),
      ).toThrow();
  }
});


it("accepts the ten built-in home styles and rejects wrong counts, names and kinds", () => {
  const homeNames = [
    "default",
    "cloud",
    "aggregate",
    "fintech",
    "signup",
    "faq",
    "startup",
    "modern",
    "assistant",
    "minimal",
  ];
  const items = homeNames.map((name) => ({ name, title: name, description: "本地内置" }));
  const list = readTemplateList({
    type: "home",
    revision: "3",
    selected: "cloud",
    items,
  });
  expect(list.items.map((i) => i.name)).toEqual(homeNames);
  expect(list.selected).toBe("cloud");
  // Wrong item count for the kind.
  expect(() =>
    readTemplateList({
      type: "home",
      revision: "3",
      selected: "default",
      items: items.slice(0, 2),
    }),
  ).toThrow();
  // Unaudited style name.
  expect(() =>
    readTemplateList({
      type: "home",
      revision: "3",
      selected: "default",
      items: [...items.slice(0, 9), { name: "evil", title: "x", description: "x" }],
    }),
  ).toThrow();
  // pay/qrpay still require exactly the two card layouts.
  expect(() =>
    readTemplateList({ type: "pay", revision: "0", selected: "default", items }),
  ).toThrow();
  // A home style name is a valid config/body name, the card names stay rejected for the wrong kind.
  expect(
    readTemplateConfig({
      type: "home",
      name: "fintech",
      revision: "0",
      config: {
        brand_name: "Local",
        notice: "",
        accent_color: "#007c91",
        background_color: "#f4f8fa",
        logo_asset_id: "",
        density: "comfortable",
      },
      fields: [],
    }).name,
  ).toBe("fintech");
  expect(() => readTemplateConfig({ type: "pay", name: "fintech", revision: "0", config: {}, fields: [] })).toThrow();
});
