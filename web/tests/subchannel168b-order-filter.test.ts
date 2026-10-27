import { it, expect } from "vitest";
import { readListRouteQuery, appendListFilters } from "../src/list-filters";
it("subchannel168b exact original order navigation is preserved through CSV query", () => {
  const r = readListRouteQuery(
    { subchannel: "9007199254740993" },
    "orders",
    "merchant",
    "1",
  );
  expect(r.error).toBe("");
  const q = new URLSearchParams();
  appendListFilters(q, r.filters, "orders", "merchant");
  expect(q.get("subchannel_id")).toBe("9007199254740993");
});
