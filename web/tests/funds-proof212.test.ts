import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";
import {
  FundsIntentStore212,
  fundsDefinitiveRefusal212,
} from "../src/funds-intent212";
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});
it.each([
  { change: {}, allowed: true },
  { change: { external_no: "other" }, allowed: false },
  { change: { merchant_id: "1002" }, allowed: false },
  { change: { reason: "guess" }, allowed: false },
])(
  "accepts only a decoded original-request-bound refusal proof: %j",
  async ({ change, allowed }) => {
    const store = new FundsIntentStore212(
      "a".repeat(64),
      "internal-transfers",
      "1000",
    );
    const intent = store.begin("/workflows/internal-transfers", {
      amount: "1.00",
      external_no: "original",
      recipient_id: "1001",
    });
    const data = {
      request_not_recorded: true,
      kind: "internal-transfers",
      merchant_id: "1000",
      external_no: "original",
      reason: "insufficient",
      ...change,
    };
    vi.stubGlobal(
      "fetch",
      async () =>
        new Response(JSON.stringify({ code: -1, message: "refused", data }), {
          status: 409,
          headers: { "Content-Type": "application/json" },
        }),
    );
    const client = new ApiClient("merchant");
    client.csrfToken = "private-proof";
    const error = await client
      .requestResult(intent.target, { method: "POST", body: intent.body })
      .catch((e) => e);
    expect(fundsDefinitiveRefusal212(error, intent)).toBe(allowed);
  },
);
