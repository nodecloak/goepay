import { describe, expect, it } from "vitest";
import { viewKind } from "../src/menu";
describe("Pro invitation management entry", () => {
  it("opens a dedicated invitation workflow instead of arbitrary resource editing", () => {
    expect(viewKind("admin", "/merchant/invite-code")).toBe("invitations");
    expect(viewKind("merchant", "/invite")).toBe("referral");
  });
});
