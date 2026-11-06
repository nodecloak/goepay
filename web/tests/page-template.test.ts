import { it, expect } from "vitest";
import {
  templateConfigBody,
  readTemplateConfig,
  readTemplateList,
  readUploadCheck,
  readUploadResult,
  readTemplateVersions,
  readRestoreResult,
  readRemoveResult,
  templateRevisionBody,
  templateSampleURL,
  templateVersionPackageURL,
  templateVersionPreviewURL,
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
];
const builtinItem = (name: string) => ({
  name,
  title: name,
  description: "本地内置",
  source: "builtin" as const,
});
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
  const items = homeNames.map(builtinItem);
  const list = readTemplateList({
    type: "home",
    revision: "3",
    selected: "index7",
    items,
  });
  expect(list.items.map((i) => i.name)).toEqual(homeNames);
  expect(list.selected).toBe("index7");
  expect(list.items.every((i) => i.source === "builtin")).toBe(true);
  // A built-in is missing (wrong count for the kind).
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
      items: [...items.slice(0, -1), builtinItem("evil")],
    }),
  ).toThrow();
  // The old "custom" home name is gone; the server must not send it as built-in.
  expect(() =>
    readTemplateList({
      type: "home",
      revision: "3",
      selected: "default",
      items: [...items, builtinItem("custom")],
    }),
  ).toThrow();
  // pay ships the two card layouts and the anime one; qrpay only the two.
  expect(() =>
    readTemplateList({
      type: "pay",
      revision: "0",
      selected: "default",
      items,
    }),
  ).toThrow();
  const card = (names: string[]) => names.map(builtinItem);
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
  expect(() =>
    readTemplateConfig({
      type: "pay",
      name: "index7",
      revision: "0",
      config: {},
      fields: [],
    }),
  ).toThrow();
});

it("accepts uploaded items beside the built-ins and validates their shape", () => {
  const key = "0123456789abcdef0123456789abcdef";
  const upload = {
    name: "my-style",
    title: "我的模板",
    description: "",
    source: "upload" as const,
    picture_url: `/template-files/${key}/preview.png`,
    upload_version: 3,
    updated_at: "2026-10-31T10:20:05Z",
    updated_by: "admin",
    files: 7,
    size: 2048,
  };
  const list = readTemplateList({
    type: "home",
    revision: "9",
    selected: "my-style",
    items: [...homeNames.map(builtinItem), upload],
  });
  const added = list.items.find((i) => i.name === "my-style")!;
  expect(added.source).toBe("upload");
  expect(added.upload_version).toBe(3);
  expect(added.picture_url).toBe(`/template-files/${key}/preview.png`);
  expect(added.files).toBe(7);
  // An empty picture_url is allowed (no preview image in the package).
  expect(
    readTemplateList({
      type: "home",
      revision: "9",
      selected: "default",
      items: [...homeNames.map(builtinItem), { ...upload, picture_url: "" }],
    }).items.find((i) => i.name === "my-style")!.picture_url,
  ).toBe("");
  // The server leaves picture_url out when there is no preview image.
  const withoutPicture: Record<string, unknown> = { ...upload };
  delete withoutPicture.picture_url;
  expect(
    readTemplateList({
      type: "home",
      revision: "9",
      selected: "default",
      items: [...homeNames.map(builtinItem), withoutPicture],
    }).items.find((i) => i.name === "my-style")!.picture_url,
  ).toBe("");
  // An uploaded name may not be a reserved/built-in name.
  for (const bad of ["custom", "index2", "preview", "Upper", "my-style-"])
    expect(() =>
      readTemplateList({
        type: "home",
        revision: "9",
        selected: "default",
        items: [...homeNames.map(builtinItem), { ...upload, name: bad }],
      }),
    ).toThrow();
  // A bad picture_url (not /template-files/<32hex>/<path>) is rejected.
  expect(() =>
    readTemplateList({
      type: "home",
      revision: "9",
      selected: "default",
      items: [
        ...homeNames.map(builtinItem),
        { ...upload, picture_url: "https://x/y.png" },
      ],
    }),
  ).toThrow();
  // upload_version must be >= 1.
  expect(() =>
    readTemplateList({
      type: "home",
      revision: "9",
      selected: "default",
      items: [...homeNames.map(builtinItem), { ...upload, upload_version: 0 }],
    }),
  ).toThrow();
  // An unknown source value is rejected.
  expect(() =>
    readTemplateList({
      type: "home",
      revision: "9",
      selected: "default",
      items: [...homeNames.map(builtinItem), { ...upload, source: "builtin" }],
    }),
  ).toThrow();
});

it("reads the upload check and result strictly", () => {
  const base = {
    type: "home",
    name: "my-style",
    title: "我的模板",
    exists: true,
    selected: false,
    next_version: 4,
    identical: false,
    files: 7,
    size: 2048,
    warnings: ["未知占位符 {{x}}"],
    removed: { "<form>": 1 },
    ignored: ["README.md"],
  };
  const check = readUploadCheck("home", base);
  expect(check.next_version).toBe(4);
  expect(check.warnings).toEqual(["未知占位符 {{x}}"]);
  expect(check.removed).toEqual({ "<form>": 1 });
  expect(check.ignored).toEqual(["README.md"]);
  // Kind mismatch, reserved name and non-boolean flags are rejected.
  expect(() => readUploadCheck("pay", base)).toThrow();
  expect(() => readUploadCheck("home", { ...base, name: "custom" })).toThrow();
  expect(() => readUploadCheck("home", { ...base, exists: "yes" })).toThrow();
  expect(() => readUploadCheck("home", { ...base, warnings: [1] })).toThrow();
  const result = readUploadResult("home", {
    type: "home",
    name: "my-style",
    title: "我的模板",
    version: 4,
    revision: "10",
    created: false,
    selected: true,
    files: 7,
    size: 2048,
    warnings: [],
    removed: {},
    ignored: [],
  });
  expect(result).toMatchObject({ version: 4, revision: "10", selected: true });
  expect(() =>
    readUploadResult("home", {
      type: "home",
      name: "my-style",
      title: "t",
      version: 0,
      revision: "10",
      created: false,
      selected: true,
      files: 0,
      size: 0,
      warnings: [],
      removed: {},
      ignored: [],
    }),
  ).toThrow();
});

it("reads the version list, restore and remove results strictly", () => {
  const rows = readTemplateVersions({
    items: [
      {
        version: 3,
        operation: "restore",
        restored_from: 1,
        title: "我的模板",
        package_version: "1.2",
        author: "me",
        files: 7,
        size: 2048,
        created_at: "2026-10-31T10:20:05Z",
        actor: "admin",
        notice: "pending",
      },
      {
        version: 2,
        operation: "upload",
        restored_from: 0,
        title: "我的模板",
        package_version: "",
        author: "",
        files: 7,
        size: 2048,
        created_at: "2026-10-31T10:00:05Z",
        actor: "admin",
        notice: "accepted",
      },
    ],
  });
  expect(rows[0].operation).toBe("restore");
  expect(rows[0].restored_from).toBe(1);
  expect(rows[1].notice).toBe("accepted");
  // restored_from must be below the version, operation and notice are enums.
  expect(() =>
    readTemplateVersions({
      items: [
        {
          version: 2,
          operation: "upload",
          restored_from: 2,
          title: "t",
          package_version: "",
          author: "",
          files: 0,
          size: 0,
          created_at: "x",
          actor: "a",
          notice: "accepted",
        },
      ],
    }),
  ).toThrow();
  expect(() =>
    readTemplateVersions({
      items: [
        {
          version: 1,
          operation: "publish",
          restored_from: 0,
          title: "t",
          package_version: "",
          author: "",
          files: 0,
          size: 0,
          created_at: "x",
          actor: "a",
          notice: "accepted",
        },
      ],
    }),
  ).toThrow();
  // Over 50 rows rejected.
  expect(() =>
    readTemplateVersions({
      items: Array.from({ length: 51 }, (_, i) => ({
        version: i + 1,
        operation: "upload",
        restored_from: 0,
        title: "t",
        package_version: "",
        author: "",
        files: 0,
        size: 0,
        created_at: "x",
        actor: "a",
        notice: "accepted",
      })),
    }),
  ).toThrow();
  expect(readRestoreResult({ version: 5, revision: "11" })).toEqual({
    version: 5,
    revision: "11",
  });
  expect(() => readRestoreResult({ version: 0, revision: "11" })).toThrow();
  expect(readRemoveResult({ revision: "12" })).toEqual({ revision: "12" });
  expect(() => readRemoveResult({ revision: "x" })).toThrow();
});

it("builds the revision body and the sample/package/preview URLs", () => {
  expect(templateRevisionBody("7")).toEqual({ expected_revision: "7" });
  expect(() => templateRevisionBody("x")).toThrow();
  expect(templateSampleURL("qrpay")).toBe(
    "/console/v1/admin/templates/qrpay/sample",
  );
  expect(templateVersionPackageURL("home", "my-style", 4)).toBe(
    "/console/v1/admin/templates/home/my-style/versions/4/package",
  );
  expect(templateVersionPreviewURL("pay", "my-style", 2)).toBe(
    "/console/v1/admin/templates/pay/my-style/versions/2/preview",
  );
  // A reserved name or a non-positive version is refused.
  expect(() => templateVersionPackageURL("home", "custom", 1)).toThrow();
  expect(() => templateVersionPreviewURL("home", "my-style", 0)).toThrow();
});
