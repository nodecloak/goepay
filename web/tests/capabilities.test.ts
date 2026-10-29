import { describe, it, expect } from "vitest";
import capabilities from "../capabilities.json";
describe("unimplemented pages remain separate from agreed plugin deferrals", () => {
  it("records dedicated sharing pages as partial and keeps remaining gaps distinct from plugin deferrals", () => {
    expect(capabilities).toHaveLength(82);
    expect(capabilities.filter((x) => x.status === "partial")).toHaveLength(81);
    expect(capabilities.filter((x) => x.status === "pending")).toHaveLength(0);
    expect(
      capabilities
        .filter((x) => x.status === "pending")
        .map((x) => `${x.area}${x.path}`)
        .sort(),
    ).toEqual([]);
    expect(
      capabilities
        .filter((x) => x.status === "deferred")
        .map((x) => `${x.area}${x.path}`)
        .sort(),
    ).toEqual(["merchant/account/certificate"]);
    for (const gap of capabilities.filter((x) => x.status === "pending")) {
      expect(gap.limitations).not.toContain("按约定");
      expect(gap.limitations).toContain("待实施");
    }
  });
});
