import { it, expect } from "vitest";
import { viewKind } from "../src/menu";
import {
  newCollectionStyle,
  readCollectionLayout,
  collectionStylePayload,
  readCollectionStyleID,
  styleImageURL,
} from "../src/collection-style";
it("both original collection style routes select the purpose-built editor/list", () => {
  expect(viewKind("admin", "/other/onecode")).toBe("collection-styles");
  expect(viewKind("admin", "/other/onecode/edit")).toBe("collection-styles");
  expect(viewKind("merchant", "/onecode")).toBe("collection-generator");
});
const valid = () => ({
  ...newCollectionStyle(),
  name: "style_1",
  label: "布局",
  url: "/collection-style-assets/" + "a".repeat(64),
});
it("normalizes numeric input controls back to the string fields required by the API", () => {
  const draft = valid();
  Object.assign(draft, { rec_name_left: 250, qr_left: 200 });
  const payload = collectionStylePayload(draft);
  expect(payload.rec_name_left).toBe("250");
  expect(payload.qr_left).toBe("200");
});
it("keeps center, transparent background and hidden recipient text as independent original values", () => {
  const layout = readCollectionLayout({
    ...valid(),
    background: "transparent",
    font_color: "transparent",
    word: "0",
  });
  expect(layout.rec_name_left).toBeNull();
  expect(layout.word).toBe(0);
  expect(layout.background).toBe("transparent");
});
it("rejects clipped or excessive canvases and fractional or negative geometry", () => {
  for (const change of [
    { qr_left: "899" },
    { img_width: "4096", img_height: "4096" },
    { qr_width: "1.5" },
    { qr_top: "-1" },
    { rec_name_left: "901" },
  ])
    expect(() => readCollectionLayout({ ...valid(), ...change })).toThrow();
});
it("rejects unsafe image schemes, credentials and CSS font injection", () => {
  for (const url of [
    "javascript:alert(1)",
    "data:image/svg+xml,<svg/>",
    "https://user:pass@example.test/image",
    "https://example.test/image#fragment",
    "/template-assets/" + "a".repeat(64),
  ])
    expect(styleImageURL(url)).toBe("");
  expect(() =>
    readCollectionLayout({
      ...valid(),
      font: "60px Arial;url(https://example.test)",
    }),
  ).toThrow();
});
it("only accepts canonical single original route IDs within bigint bounds", () => {
  expect(readCollectionStyleID(undefined)).toBeUndefined();
  expect(readCollectionStyleID("9223372036854775807")).toBe(
    "9223372036854775807",
  );
  for (const bad of ["0", "01", "9223372036854775808", ["7", "8"], null])
    expect(readCollectionStyleID(bad)).toBe("");
});
it("rejects mismatched font quotes instead of silently falling back to a different canvas font", () => {
  expect(() =>
    readCollectionLayout({ ...valid(), font: `60px '黑体"` }),
  ).toThrow();
});
