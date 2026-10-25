// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import PreauthorizationObservation from "../src/components/PreauthorizationObservation.vue";

const order = {
  id: "9007199254740993123",
  merchant_id: "1000",
  trade_no: "AUTH-77",
  amount: "9007199254740993",
};
const requestID = "a".repeat(64);
const result = (nonce: string, changes: Record<string, unknown> = {}) => ({
  request_id: requestID,
  client_request_no: nonce,
  merchant_id: order.merchant_id,
  order_id: order.id,
  attempt_id: "8",
  channel_id: "7",
  trade_no: order.trade_no,
  app_id: "APP-1",
  auth_no: "AUTH-NO-77",
  buyer_id: "BUYER-77",
  amount: "9007199254740993",
  status: "unknown",
  rest_known: false,
  provider_status: "",
  order_status: "pending",
  operation_type: "",
  source_verified: false,
  ...changes,
});
const setup = (client = new ApiClient("merchant")) =>
  mount(PreauthorizationObservation, {
    props: { client, order, allowed: true, ownSession: true },
  });
const button = (w: ReturnType<typeof setup>, text: string) =>
  w.findAll("button").find((b) => b.text() === text)!;

describe("durable preauthorization read-only observations", () => {
  it("requires a separate explicit remaining confirmation and preserves the exact observed cents and revision", async () => {
    const client = new ApiClient("admin");
    vi.spyOn(client, "request").mockImplementation(
      async (_path, options) =>
        result(
          (options?.body as { client_request_no: string }).client_request_no,
          {
            status: "verified",
            source_verified: true,
            rest_known: true,
            rest_amount: "450",
            provider_status: "SUCCESS",
            order_status: "AUTHORIZED",
            operation_type: "FREEZE",
            observed_at: "2026-10-24T06:00:00Z",
          },
        ) as never,
    );
    const send = vi
      .spyOn(client, "requestResult")
      .mockRejectedValue(new Error("synthetic unknown response"));
    const w = mount(PreauthorizationObservation, {
      props: {
        client,
        order,
        allowed: true,
        ownSession: true,
        releaseAllowed: true,
        releaseRevision: "2",
      },
    });
    await button(w, "查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(send).not.toHaveBeenCalled();
    await button(w, "解除剩余冻结").trigger("click");
    expect(send).not.toHaveBeenCalled();
    expect(w.text()).toContain("本次剩余解除额（元）4.50");
    await button(w, "确认解除本次剩余额度").trigger("click");
    await flushPromises();
    expect(send).toHaveBeenCalledExactlyOnceWith(
      "/orders/AUTH-77/release-remaining-authorization",
      {
        method: "POST",
        body: {
          expected_observation_id: requestID,
          expected_remaining_cents: "450",
          expected_source_revision: "2",
        },
      },
    );
    expect(w.emitted("released")?.[0]?.[0]).toContain("尚未核实");
    expect(w.findAll("button").some((b) => b.text() === "解除剩余冻结")).toBe(
      false,
    );
    w.unmount();
  });
  it("accepts omitted optional provider status fields without inventing a remaining amount", async () => {
    const client = new ApiClient("merchant");
    vi.spyOn(client, "request").mockImplementation(async (_path, options) => {
      const dto = result(
        (options?.body as { client_request_no: string }).client_request_no,
      );
      delete (dto as Record<string, unknown>).provider_status;
      delete (dto as Record<string, unknown>).order_status;
      delete (dto as Record<string, unknown>).operation_type;
      return dto as never;
    });
    const w = setup(client);
    await button(w, "查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(w.text()).toContain("AUTH-NO-77");
    expect(w.find('[role="alert"]').exists()).toBe(false);
    expect(w.text()).toContain("余款未知");
    w.unmount();
  });
  it("is default-off, refuses delegation and never queries during render", async () => {
    const client = new ApiClient("merchant"),
      request = vi.spyOn(client, "request");
    const w = mount(PreauthorizationObservation, { props: { client, order } });
    expect(w.find("button").exists()).toBe(false);
    await w.setProps({ allowed: true, ownSession: false });
    expect(w.find("button").exists()).toBe(false);
    expect(request).not.toHaveBeenCalled();
    w.unmount();
  });
  it("claims a fresh nonce only on explicit query and refreshes unknown by original GET without another POST", async () => {
    const client = new ApiClient("merchant");
    let nonce = "";
    const request = vi
      .spyOn(client, "request")
      .mockImplementation(async (_path, options) => {
        if (options?.method === "POST")
          nonce = (options.body as { client_request_no: string })
            .client_request_no;
        return result(nonce) as never;
      });
    const w = setup(client);
    await button(w, "查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(nonce).toMatch(/^[0-9a-f]{64}$/);
    expect(request.mock.calls[0][0]).toBe(
      "/orders/AUTH-77/authorization-observations",
    );
    expect(request.mock.calls[0][1]?.body).toEqual({
      client_request_no: nonce,
    });
    expect(w.text()).toContain("余款未知");
    expect(w.text()).not.toContain("余款（元）0.00");
    await button(w, "刷新原查询状态").trigger("click");
    await flushPromises();
    expect(request.mock.calls[1][0]).toBe(
      `/orders/AUTH-77/authorization-observations/${requestID}`,
    );
    expect(request.mock.calls[1][1]?.method ?? "GET").toBe("GET");
    expect(
      request.mock.calls.filter(([, o]) => o?.method === "POST"),
    ).toHaveLength(1);
    const original = nonce;
    await button(w, "重新查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(nonce).not.toBe(original);
    expect(
      request.mock.calls.filter(([, o]) => o?.method === "POST"),
    ).toHaveLength(2);
    w.unmount();
  });
  it("distinguishes verified zero from unknown, renders exact amount and historical provenance", async () => {
    const client = new ApiClient("merchant");
    vi.spyOn(client, "request").mockImplementation(
      async (_path, options) =>
        result(
          (options?.body as { client_request_no: string }).client_request_no,
          {
            status: "verified",
            rest_known: true,
            rest_amount: "0",
            source_verified: true,
            observed_at: "2026-10-24T00:00:00Z",
            provider_status: "SUCCESS",
            order_status: "AUTHORIZED",
            operation_type: "FREEZE",
          },
        ) as never,
    );
    const w = setup(client);
    await button(w, "查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(w.text()).toContain("原冻结金额（元）90071992547409.93");
    expect(w.text()).toContain("余款（元）0.00");
    expect(w.text()).toContain("来源已验证");
    expect(w.text()).toContain("2026-10-24 08:00:00");
    expect(w.text()).toContain("观察时间（北京时间）");
    expect(w.text()).toContain("不会扣款或解除冻结");
    w.unmount();
  });
  it.each([
    { merchant_id: "1001" },
    { order_id: "2" },
    { trade_no: "OTHER" },
    { client_request_no: "b".repeat(64) },
    { request_id: "../other" },
    { amount: 9007199254740992 },
    { amount: "100" },
    {
      status: "verified",
      source_verified: true,
      rest_known: false,
      observed_at: "2026-10-24T00:00:00Z",
    },
    { status: "unknown", source_verified: true },
    { status: "processing", source_verified: true },
    {
      status: "unknown",
      rest_known: true,
      rest_amount: "0",
      source_verified: false,
    },
  ])(
    "rejects mismatched or unverified observation proof %j",
    async (changes) => {
      const client = new ApiClient("merchant");
      vi.spyOn(client, "request").mockImplementation(
        async (_path, options) =>
          result(
            (options?.body as { client_request_no: string }).client_request_no,
            changes,
          ) as never,
      );
      const w = setup(client);
      await button(w, "查询原冻结与余款").trigger("click");
      await flushPromises();
      expect(w.find('[role="alert"]').exists()).toBe(true);
      expect(w.text()).not.toContain("BUYER-77");
      expect(w.text()).not.toContain("余款（元）0.00");
      w.unmount();
    },
  );
  it("does not retry a lost POST response and explicit requery uses a new nonce", async () => {
    const client = new ApiClient("merchant");
    const request = vi
      .spyOn(client, "request")
      .mockRejectedValue(new Error("lost response"));
    const w = setup(client);
    await button(w, "查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(request).toHaveBeenCalledTimes(1);
    expect(w.text()).toContain("结果未知");
    expect(button(w, "刷新原查询状态")).toBeUndefined();
    const nonce = (
      request.mock.calls[0][1]?.body as { client_request_no: string }
    ).client_request_no;
    await button(w, "重新查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(
      (request.mock.calls[1][1]?.body as { client_request_no: string })
        .client_request_no,
    ).not.toBe(nonce);
    w.unmount();
  });
  it.each(["order", "account", "area", "permission", "delegation"])(
    "aborts and ignores late results after %s changes",
    async (context) => {
      const client = new ApiClient("merchant");
      let finish!: (data: unknown) => void;
      const request = vi.spyOn(client, "request").mockImplementation(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      );
      const w = setup(client);
      await button(w, "查询原冻结与余款").trigger("click");
      const nonce = (
        request.mock.calls[0][1]?.body as { client_request_no: string }
      ).client_request_no;
      const signal = request.mock.calls[0][1]?.signal;
      if (context === "order")
        await w.setProps({
          order: { id: "88", merchant_id: "2000", trade_no: "AUTH-88" },
        });
      else if (context === "account")
        await w.setProps({ client: new ApiClient("merchant") });
      else if (context === "area")
        await w.setProps({ client: new ApiClient("admin") });
      else if (context === "permission") await w.setProps({ allowed: false });
      else await w.setProps({ ownSession: false });
      expect(signal?.aborted).toBe(true);
      finish(result(nonce));
      await flushPromises();
      expect(w.text()).not.toContain("BUYER-77");
      if (["order", "account", "area"].includes(context))
        expect(button(w, "查询原冻结与余款")).toBeDefined();
      else {
        expect(w.find("button").exists()).toBe(false);
        await w.setProps({ allowed: true, ownSession: true });
        expect(w.text()).not.toContain("BUYER-77");
      }
      await w.setProps({ allowed: false });
      expect(w.find("button").exists()).toBe(false);
      w.unmount();
    },
  );
  it("rejects another request returned by a local refresh and does not issue a replacement POST", async () => {
    const client = new ApiClient("merchant");
    let nonce = "";
    const request = vi
      .spyOn(client, "request")
      .mockImplementation(async (_path, options) => {
        if (options?.method === "POST") {
          nonce = (options.body as { client_request_no: string })
            .client_request_no;
          return result(nonce) as never;
        }
        return result(nonce, { request_id: "b".repeat(64) }) as never;
      });
    const w = setup(client);
    await button(w, "查询原冻结与余款").trigger("click");
    await flushPromises();
    expect(w.text()).toContain("BUYER-77");
    await button(w, "刷新原查询状态").trigger("click");
    await flushPromises();
    expect(w.find('[role="alert"]').exists()).toBe(true);
    expect(w.text()).not.toContain("BUYER-77");
    expect(
      request.mock.calls.filter(([, options]) => options?.method === "POST"),
    ).toHaveLength(1);
    w.unmount();
  });
  it("blocks duplicate clicks during the original send and aborts it on close", async () => {
    const client = new ApiClient("merchant");
    const request = vi
      .spyOn(client, "request")
      .mockImplementation(() => new Promise(() => {}));
    const w = setup(client);
    await button(w, "查询原冻结与余款").trigger("click");
    await button(w, "重新查询原冻结与余款").trigger("click");
    expect(request).toHaveBeenCalledTimes(1);
    const signal = request.mock.calls[0][1]?.signal;
    w.unmount();
    expect(signal?.aborted).toBe(true);
  });
});
