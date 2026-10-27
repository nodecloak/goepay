import { it, expect } from "vitest";
import fixture from "../e2e/subchannel168b-fixture.json";
import { readSubchannelRow } from "../src/subchannels";
it("subchannel168b rejects an archived fresh detail before any mutation", () => {
  expect(() =>
    readSubchannelRow(
      {
        ...fixture.merchant.items[0],
        enabled: false,
        archived: true,
        browser_test_available: false,
        unavailable_reason: "disabled",
      },
      "merchant",
      "1",
    ),
  ).toThrow();
});
