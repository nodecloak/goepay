import { describe, it, expect } from "vitest";
import { navigationFor, pageAuthorizedFor, flattenMenu } from "../src/menu";

// internal_transfer / recharge / onecode default ON (server sends true unless
// turned off); complain_open / applyments_open / mchrisk_open default OFF
// (server sends false unless turned on). The menu hides a feature's entry when
// its flag is false, and shows it when true or absent — mirroring the original
// user/head.php `$conf['x']==1` guards.
describe("merchant feature menu-hide", () => {
  const ids = (features?: Record<string, boolean>) =>
    flattenMenu(
      navigationFor("merchant", "merchant", undefined, undefined, features),
    ).map((n) => n.id);

  it("shows all feature entries when no flags are supplied", () => {
    const all = ids();
    for (const id of ["InternalTransfer", "Recharge", "Onecode", "Complain"])
      expect(all, id).toContain(id);
  });

  it("hides an entry whose feature flag is false", () => {
    const shown = ids({
      internal_transfer: false,
      recharge: false,
      onecode: false,
      complain_open: false,
      applyments_open: false,
      mchrisk_open: false,
    });
    for (const id of [
      "InternalTransfer",
      "Recharge",
      "Onecode",
      "Complain",
      "ApplymentsManage",
      "MchRisk",
    ])
      expect(shown, id).not.toContain(id);
  });

  it("keeps an entry whose feature flag is true", () => {
    const shown = ids({ recharge: true, onecode: false });
    expect(shown).toContain("Recharge");
    expect(shown).not.toContain("Onecode");
  });

  it("drops the 商户进件 group when all its children are hidden", () => {
    const nav = navigationFor("merchant", "merchant", undefined, undefined, {
      applyments_open: false,
      mchrisk_open: false,
    });
    expect(nav.some((n) => n.id === "ApplymentsManage")).toBe(false);
  });

  it("refuses the page route of a feature that is off", () => {
    expect(
      pageAuthorizedFor(
        "merchant",
        "/onecode",
        "merchant",
        undefined,
        undefined,
        { onecode: false },
      ),
    ).toBe(false);
    expect(
      pageAuthorizedFor(
        "merchant",
        "/onecode",
        "merchant",
        undefined,
        undefined,
        { onecode: true },
      ),
    ).toBe(true);
    // No flags supplied → not narrowed.
    expect(
      pageAuthorizedFor(
        "merchant",
        "/onecode",
        "merchant",
        undefined,
        undefined,
      ),
    ).toBe(true);
  });
});
