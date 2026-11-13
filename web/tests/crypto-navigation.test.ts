import { expect, it } from "vitest";
import {
  flattenMenu,
  navigationFor,
  pageAuthorizedFor,
  viewKind,
} from "../src/menu";

it("separates USDT workflows into a dedicated navigation group with role-bound routes", () => {
  for (const area of ["admin", "merchant"] as const) {
    const navigation = navigationFor(
      area,
      area,
      area === "admin" ? "super" : undefined,
      undefined,
    );
    const group = navigation.find((n) => n.id === "CryptoWorkspace");
    expect(group?.title).toBe("USDT 资金");
    const base = area === "admin" ? "/system/crypto" : "/account/crypto";
    const sections = [
      "",
      "/invoices",
      "/settlements",
      "/refunds",
      "/recipients",
      ...(area === "admin" ? ["/operations", "/reconciliation"] : []),
    ];
    expect(group?.children.map((n) => n.path)).toEqual(
      sections.map((s) => base + s),
    );
    for (const section of sections) {
      expect(viewKind(area, base + section)).toBe("crypto");
      expect(
        pageAuthorizedFor(
          area,
          base + section,
          area,
          area === "admin" ? "super" : undefined,
          undefined,
        ),
      ).toBe(true);
      if (area === "admin")
        expect(
          pageAuthorizedFor(
            area,
            base + section,
            "admin",
            "operator",
            undefined,
          ),
        ).toBe(false);
    }
    expect(
      flattenMenu(navigation).filter((n) => n.path.startsWith(base)),
    ).toHaveLength(sections.length);
    expect(viewKind(area, base + "/unknown")).toBe("unavailable");
    expect(
      pageAuthorizedFor(area, base + "/unknown", area, "super", undefined),
    ).toBe(false);
  }
  expect(
    pageAuthorizedFor(
      "merchant",
      "/account/crypto/operations",
      "merchant",
      undefined,
      undefined,
    ),
  ).toBe(false);
  expect(
    pageAuthorizedFor(
      "merchant",
      "/account/crypto/reconciliation",
      "merchant",
      undefined,
      undefined,
    ),
  ).toBe(false);
});

it("opens a merchant USDT funds page and reserves platform controls for super administrators", () => {
  expect(viewKind("merchant", "/account/crypto")).toBe("crypto");
  expect(viewKind("admin", "/system/crypto")).toBe("crypto");
  expect(
    pageAuthorizedFor(
      "merchant",
      "/account/crypto",
      "merchant",
      undefined,
      undefined,
    ),
  ).toBe(true);
  expect(
    pageAuthorizedFor(
      "admin",
      "/system/crypto",
      "admin",
      "operator",
      undefined,
    ),
  ).toBe(false);
  expect(
    pageAuthorizedFor("admin", "/system/crypto", "admin", "super", undefined),
  ).toBe(true);
  expect(
    JSON.stringify(navigationFor("merchant", "merchant", undefined, undefined)),
  ).toContain("USDT");
});
