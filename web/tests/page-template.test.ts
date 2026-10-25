import { it, expect } from "vitest";
import { templateConfigBody, readTemplateConfig } from "../src/page-template";
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
