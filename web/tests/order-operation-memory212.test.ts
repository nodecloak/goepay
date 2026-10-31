import { describe, expect, it } from "vitest";
import { reactive } from "vue";
import { ApiClient } from "../src/api";
import { orderOperationMemory212 } from "../src/order-operation-memory212";

describe("pending order operation session boundaries", () => {
  it("restores the same operation through Vue proxies without crossing order or action", () => {
    const client = new ApiClient("admin");
    client.csrfToken = "session-A";
    orderOperationMemory212(client, "income", "ORDER-A").save({
      request: "original-A",
    });
    expect(
      orderOperationMemory212(reactive(client), "income", "ORDER-A").read(),
    ).toEqual({ request: "original-A" });
    expect(
      orderOperationMemory212(client, "income", "ORDER-B").read(),
    ).toBeUndefined();
    expect(
      orderOperationMemory212(client, "notify", "ORDER-A").read(),
    ).toBeUndefined();
  });
  it("credential changes discard old state and prevent a stale dialog saving into the next session", () => {
    const client = new ApiClient("admin");
    client.csrfToken = "session-A";
    const old = orderOperationMemory212(client, "income", "ORDER-A");
    old.save({ request: "old" });
    client.csrfToken = "session-B";
    const current = orderOperationMemory212(client, "income", "ORDER-A");
    expect(current.read()).toBeUndefined();
    old.save({ request: "late-old-result" });
    expect(current.read()).toBeUndefined();
    current.save({ request: "new" });
    old.clear();
    expect(current.read()).toEqual({ request: "new" });
  });
  it("different ApiClient sessions never share pending operation state", () => {
    const first = new ApiClient("admin"),
      second = new ApiClient("admin");
    first.csrfToken = second.csrfToken = "synthetic-proof";
    orderOperationMemory212(first, "notify", "ORDER-A").save({
      request: "original",
    });
    expect(
      orderOperationMemory212(second, "notify", "ORDER-A").read(),
    ).toBeUndefined();
  });
});
