import { it, expect } from "vitest";
import { freshListFilters, appendListFilters } from "../src/list-filters";
it("scopes income status to orders and clears it in fresh filters", () => {
  expect((freshListFilters() as Record<string, string>).income_status).toBe("");
  const filters = { ...freshListFilters(), income_status: "held" };
  const order = new URLSearchParams();
  appendListFilters(order, filters, "orders", "admin");
  expect(order.get("income_status")).toBe("held");
  const refund = new URLSearchParams();
  appendListFilters(refund, filters, "refunds", "admin");
  expect(refund.has("income_status")).toBe(false);
});
