import { describe, expect, it } from "vitest";
import { builtinTemplates } from "../src/plugin-catalog";
import { templatePreviewImage } from "../src/page-template";

describe("published homepage preview cache version", () => {
  for (const entry of builtinTemplates.filter((item) => item.type === "home")) {
    it(`updates the cached ${entry.name} picture using its registry version`, () => {
      const image = new URL(
        templatePreviewImage(entry.type, entry.name),
        "https://console.example",
      );
      expect(image.origin).toBe("https://console.example");
      expect(image.pathname).toBe(
        `/static/template-previews/home-${entry.name}.jpg`,
      );
      expect(image.searchParams.get("v")).toBe(entry.version);
      expect([...image.searchParams.keys()]).toEqual(["v"]);
    });
  }

  it("preserves checkout preview URLs and does not invent a version for unknown names", () => {
    for (const kind of ["pay", "qrpay"]) {
      expect(templatePreviewImage(kind, "default")).toBe(
        `/static/template-previews/${kind}-default.jpg`,
      );
    }
    expect(templatePreviewImage("home", "not-installed")).toBe(
      "/static/template-previews/home-not-installed.jpg",
    );
  });
});
