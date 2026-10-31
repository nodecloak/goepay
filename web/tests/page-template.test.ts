import { it, expect } from "vitest";
import {
  templateConfigBody,
  readTemplateConfig,
  readTemplateList,
  customHtmlBody,
  customHtmlByteLength,
  readCustomHtml,
  readCustomSample,
  readCustomSaveResult,
  readCustomVersions,
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


it("accepts the built-in home styles and rejects wrong counts, names and kinds", () => {
  const homeNames = [
    "default",
    "index1",
    "index2",
    "index3",
    "index4",
    "index5",
    "index6",
    "index7",
    "index8",
    "index9",
    "index10",
    "minimal",
    "sakura",
    "starry",
    "manga",
    "mecha",
    "candy",
    "custom",
  ];
  const items = homeNames.map((name) => ({ name, title: name, description: "本地内置" }));
  const list = readTemplateList({
    type: "home",
    revision: "3",
    selected: "index7",
    items,
  });
  expect(list.items.map((i) => i.name)).toEqual(homeNames);
  expect(list.selected).toBe("index7");
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
      items: [...items.slice(0, -1), { name: "evil", title: "x", description: "x" }],
    }),
  ).toThrow();
  // pay ships the two card layouts and the anime one; qrpay only the two.
  expect(() =>
    readTemplateList({ type: "pay", revision: "0", selected: "default", items }),
  ).toThrow();
  const card = (names: string[]) =>
    names.map((name) => ({ name, title: name, description: "本地内置" }));
  expect(
    readTemplateList({
      type: "pay",
      revision: "0",
      selected: "anime",
      items: card(["default", "compact", "anime"]),
    }).items.map((i) => i.name),
  ).toEqual(["default", "compact", "anime"]);
  expect(() =>
    readTemplateList({
      type: "qrpay",
      revision: "0",
      selected: "default",
      items: card(["default", "compact", "anime"]),
    }),
  ).toThrow();
  expect(
    readTemplateList({
      type: "qrpay",
      revision: "0",
      selected: "default",
      items: card(["default", "compact"]),
    }).items,
  ).toHaveLength(2);
  // A home style name is a valid config/body name, the card names stay rejected for the wrong kind.
  expect(
    readTemplateConfig({
      type: "home",
      name: "index7",
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
  ).toBe("index7");
  expect(() => readTemplateConfig({ type: "pay", name: "index7", revision: "0", config: {}, fields: [] })).toThrow();
});

it("accepts the custom home name for config/list/body", () => {
  expect(
    readTemplateList({
      type: "home",
      revision: "0",
      selected: "custom",
      items: [
        "default",
        "index1",
        "index2",
        "index3",
        "index4",
        "index5",
        "index6",
        "index7",
        "index8",
        "index9",
        "index10",
        "minimal",
        "sakura",
        "starry",
        "manga",
        "mecha",
        "candy",
        "custom",
      ].map((name) => ({ name, title: name, description: "本地" })),
    }).selected,
  ).toBe("custom");
  expect(
    readTemplateConfig({
      type: "home",
      name: "custom",
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
  ).toBe("custom");
});

it("counts UTF-8 bytes and enforces the custom HTML/CSS limits", () => {
  expect(customHtmlByteLength("中")).toBe(3);
  expect(customHtmlByteLength("ab")).toBe(2);
  expect(
    customHtmlBody("12", "<p>hi</p>", ".a{color:red}", true),
  ).toEqual({
    expected_revision: "12",
    html: "<p>hi</p>",
    css: ".a{color:red}",
    frame: true,
  });
  // Byte, not code-point, limits.
  expect(() => customHtmlBody("1", "a".repeat(65537), "", true)).toThrow(
    "HTML 超过 64 KiB",
  );
  expect(() => customHtmlBody("1", "中".repeat(21846), "", true)).toThrow(
    "HTML 超过 64 KiB",
  );
  expect(() => customHtmlBody("1", "", "a".repeat(32769), false)).toThrow(
    "CSS 超过 32 KiB",
  );
  // A boundary value (exactly the limit) is allowed.
  expect(customHtmlBody("1", "a".repeat(65536), "a".repeat(32768), true).html.length).toBe(
    65536,
  );
  // Bad revision / types rejected.
  expect(() => customHtmlBody("x", "", "", true)).toThrow();
  expect(() => customHtmlBody("1", "", "", "yes" as any)).toThrow();
});

it("reads the custom HTML head strictly, including the empty version 0", () => {
  const empty = readCustomHtml({ version: 0, revision: "0", html: "", css: "", frame: true });
  expect(empty.version).toBe(0);
  expect(empty.frame).toBe(true);
  const v = readCustomHtml({
    version: 4,
    revision: "13",
    html: "<p>x</p>",
    css: "",
    frame: false,
    created_at: "2026-10-31T10:20:05Z",
    actor: "admin",
    restored_from: 2,
  });
  expect(v).toMatchObject({ version: 4, revision: "13", restored_from: 2, frame: false });
  // restored_from must be below the version.
  expect(() =>
    readCustomHtml({
      version: 2,
      revision: "3",
      html: "",
      css: "",
      frame: true,
      created_at: "2026-10-31T10:20:05Z",
      actor: "admin",
      restored_from: 2,
    }),
  ).toThrow();
  // A non-empty version with wrong types is rejected.
  expect(() =>
    readCustomHtml({ version: 1, revision: "2", html: 5, css: "", frame: true, created_at: "x", actor: "a" }),
  ).toThrow();
});

it("reads the custom sample, versions and save result, rejecting bad shapes", () => {
  expect(readCustomSample({ html: "<p>x</p>", css: "", frame: true }).frame).toBe(true);
  expect(() => readCustomSample({ html: "<p>x</p>", css: "", frame: "no" })).toThrow();
  const rows = readCustomVersions({
    items: [
      {
        version: 4,
        created_at: "2026-10-31T10:20:05Z",
        actor: "admin",
        html_bytes: 11,
        css_bytes: 0,
        frame: true,
        restored_from: 0,
        notice: "pending",
      },
    ],
  });
  expect(rows[0].notice).toBe("pending");
  // Unknown notice state rejected.
  expect(() =>
    readCustomVersions({
      items: [
        {
          version: 1,
          created_at: "x",
          actor: "a",
          html_bytes: 0,
          css_bytes: 0,
          frame: true,
          restored_from: 0,
          notice: "sent",
        },
      ],
    }),
  ).toThrow();
  // Over 50 items rejected.
  expect(() =>
    readCustomVersions({
      items: Array.from({ length: 51 }, (_, i) => ({
        version: i + 1,
        created_at: "x",
        actor: "a",
        html_bytes: 0,
        css_bytes: 0,
        frame: true,
        restored_from: 0,
        notice: "accepted",
      })),
    }),
  ).toThrow();
  expect(
    readCustomSaveResult({ version: 4, revision: "13", removed: { "<script>": 1 } }).removed,
  ).toEqual({ "<script>": 1 });
  // Missing removed defaults to empty.
  expect(readCustomSaveResult({ version: 1, revision: "2" }).removed).toEqual({});
  // version 0 is not a valid save result.
  expect(() => readCustomSaveResult({ version: 0, revision: "1" })).toThrow();
  // Non-numeric removed counts rejected.
  expect(() =>
    readCustomSaveResult({ version: 1, revision: "2", removed: { onclick: "two" } }),
  ).toThrow();
});
