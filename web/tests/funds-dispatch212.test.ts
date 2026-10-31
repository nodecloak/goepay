import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";

afterEach(() => vi.unstubAllGlobals());
const client = (area: "admin" | "merchant" = "admin") => {
  const api = new ApiClient(area);
  api.csrfToken = "not-persisted-proof";
  return api;
};
const reply = () =>
  new Response(JSON.stringify({ code: 0, data: { status: "pending" } }), {
    headers: { "Content-Type": "application/json" },
  });

it("persists immediately before the actual business fetch, after successful preflight", async () => {
  const events: string[] = [],
    api = client();
  vi.stubGlobal("fetch", async () => {
    events.push("fetch");
    return reply();
  });
  api.beforeFundsAction = async (_action, send) => {
    events.push("preflight");
    return send();
  };
  await api.requestResult("/orders/T/refund", {
    method: "POST",
    body: { amount: "1.00" },
    beforeDispatch: () => events.push("save"),
  });
  expect(events).toEqual(["preflight", "save", "fetch"]);
});

it("does not persist a sent intent when preflight rejects before dispatch", async () => {
  const api = client(),
    persist = vi.fn(),
    fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  api.beforeFundsAction = async () => {
    throw new Error("status unavailable");
  };
  await expect(
    api.requestResult("/orders/T/refund", {
      method: "POST",
      body: { amount: "1.00" },
      beforeDispatch: persist,
    }),
  ).rejects.toThrow("status unavailable");
  expect(persist).not.toHaveBeenCalled();
  expect(fetch).not.toHaveBeenCalled();
});

it("passes persistence to the authorized original-target execution, including a lost response", async () => {
  const events: string[] = [],
    api = client();
  vi.stubGlobal("fetch", async () => {
    events.push("authorized-fetch");
    throw new TypeError("lost response");
  });
  api.beforeFundsAction = async (action) =>
    api.requestResult("/orders/T/refund", {
      method: "POST",
      body: action.body,
      paymentAuthorization: "ephemeral-grant",
      beforeDispatch: action.beforeDispatch,
    });
  await expect(
    api.requestResult("/orders/T/refund", {
      method: "POST",
      body: { amount: "1.00" },
      beforeDispatch: () => events.push("save"),
    }),
  ).rejects.toThrow("lost response");
  expect(events).toEqual(["save", "authorized-fetch"]);
});

it("does not send a business POST if persistence throws", async () => {
  const api = client("merchant"),
    fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    api.requestResult("/orders/T/refund", {
      method: "POST",
      body: { amount: "1.00" },
      beforeDispatch: () => {
        throw new Error("storage failed");
      },
    }),
  ).rejects.toThrow("storage failed");
  expect(fetch).not.toHaveBeenCalled();
});

it("does not save a sent intent for an already cancelled request", async () => {
  const api = client("merchant"),
    persist = vi.fn(),
    fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const controller = new AbortController();
  controller.abort(new Error("cancelled before dispatch"));
  await expect(
    api.requestResult("/orders/T/refund", {
      method: "POST",
      body: { amount: "1.00" },
      signal: controller.signal,
      beforeDispatch: persist,
    }),
  ).rejects.toThrow("cancelled before dispatch");
  expect(persist).not.toHaveBeenCalled();
  expect(fetch).not.toHaveBeenCalled();
});
