// @vitest-environment jsdom
import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import MerchantGroupPicker from "../src/components/MerchantGroupPicker.vue";

it("a missing current group stays selected until an explicit choice and never makes a write", async () => {
  const client = new ApiClient("admin"),
    calls = vi.spyOn(client, "request").mockImplementation(async (path) => {
      if (path.startsWith("/resources/groups?"))
        return { items: [{ id: "1", name: "普通组" }], total: 1 } as never;
      throw Error("current group unavailable");
    });
  const w = mount(MerchantGroupPicker, {
    props: { client, value: "9223372036854775807", disabled: false },
  });
  await flushPromises();
  expect((w.find("select").element as HTMLSelectElement).value).toBe(
    "9223372036854775807",
  );
  expect(w.text()).toContain("current group unavailable");
  expect(w.emitted("change")).toBeUndefined();
  await w.find("select").setValue("");
  expect(w.emitted("change")).toEqual([[""]]);
  expect(calls.mock.calls.every(([, o]) => !o?.method)).toBe(true);
  w.unmount();
});
it("pagination and an Enter search use bounded GETs without clearing the chosen group or submitting a form", async () => {
  const client = new ApiClient("admin"),
    calls = vi.spyOn(client, "request").mockImplementation(async (path) => {
      if (path === "/resources/groups/5")
        return { id: "5", name: "当前组" } as never;
      const q = new URL(path, "http://localhost").searchParams;
      return {
        items: [
          {
            id: q.get("page") === "2" ? "2" : "1",
            name: q.get("q") || "普通组",
          },
        ],
        total: 101,
      } as never;
    });
  const w = mount(MerchantGroupPicker, {
    props: { client, value: "5", disabled: false },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "下页用户组")!
    .trigger("click");
  await flushPromises();
  expect(
    new URL(calls.mock.calls.at(-1)![0], "http://localhost").searchParams.get(
      "page",
    ),
  ).toBe("2");
  const input = w.find('[aria-label="搜索用户组名称"]');
  await input.setValue("收费组");
  const event = new KeyboardEvent("keydown", {
    key: "Enter",
    bubbles: true,
    cancelable: true,
  });
  expect(input.element.dispatchEvent(event)).toBe(false);
  await flushPromises();
  const q = new URL(calls.mock.calls.at(-1)![0], "http://localhost")
    .searchParams;
  expect(q.get("page")).toBe("1");
  expect(q.get("limit")).toBe("100");
  expect(q.get("q")).toBe("收费组");
  expect(w.emitted("change")).toBeUndefined();
  expect((w.find("select").element as HTMLSelectElement).value).toBe("5");
  w.unmount();
});
it("an aborted previous-account list cannot restore its labels or selection", async () => {
  const old = new ApiClient("admin"),
    current = new ApiClient("admin");
  let finish!: (value: unknown) => void;
  const before = vi.spyOn(old, "request").mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }) as never,
  );
  vi.spyOn(current, "request").mockImplementation(
    async (path) =>
      (path.includes("?")
        ? { items: [{ id: "2", name: "新账号组" }], total: 1 }
        : { id: "2", name: "新账号组" }) as never,
  );
  const w = mount(MerchantGroupPicker, {
    props: { client: old, value: "", disabled: false },
  });
  await flushPromises();
  await w.setProps({ client: current, value: "2" });
  await flushPromises();
  finish({ items: [{ id: "1", name: "旧账号组" }], total: 1 });
  await flushPromises();
  expect(before.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
  expect(w.text()).toContain("新账号组");
  expect(w.text()).not.toContain("旧账号组");
  expect((w.find("select").element as HTMLSelectElement).value).toBe("2");
  expect(w.emitted("change")).toBeUndefined();
  w.unmount();
});

it("a failed group search leaves its search controls available to correct and retry", async () => {
  const client = new ApiClient("admin");
  let fail = false;
  vi.spyOn(client, "request").mockImplementation(async (path) => {
    if (fail) throw Error("temporary group lookup failure");
    const filtered = new URL(path, "http://localhost").searchParams.has("q");
    return {
      items: filtered ? [] : [{ id: "1", name: "普通组" }],
      total: filtered ? 0 : 101,
    } as never;
  });
  const w = mount(MerchantGroupPicker, {
    props: { client, value: "", disabled: false },
  });
  await flushPromises();
  const input = w.get('[aria-label="搜索用户组名称"]');
  await input.setValue("缺失的组");
  fail = true;
  await input.trigger("keydown", { key: "Enter" });
  await flushPromises();
  expect(w.text()).toContain("temporary group lookup failure");
  await w.get('[aria-label="搜索用户组名称"]').setValue("");
  fail = false;
  await w
    .get('[aria-label="搜索用户组名称"]')
    .trigger("keydown", { key: "Enter" });
  await flushPromises();
  expect(w.find('[aria-label="搜索用户组名称"]').exists()).toBe(true);
  expect(w.emitted("change")).toBeUndefined();
  expect((w.get("select").element as HTMLSelectElement).disabled).toBe(false);
  w.unmount();
});

it("paging keeps the search control mounted while a list request is pending", async () => {
  const client = new ApiClient("admin");
  let finish!: (value: unknown) => void,
    calls = 0;
  vi.spyOn(client, "request").mockImplementation(async () => {
    if (++calls === 1)
      return { items: [{ id: "1", name: "普通组" }], total: 101 } as never;
    return new Promise((resolve) => {
      finish = resolve;
    }) as never;
  });
  const w = mount(MerchantGroupPicker, {
    props: { client, value: "", disabled: false },
  });
  await flushPromises();
  const input = w.get('[aria-label="搜索用户组名称"]').element;
  await w
    .findAll("button")
    .find((b) => b.text() === "查询用户组")!
    .trigger("click");
  expect(w.find('[aria-label="搜索用户组名称"]').element).toBe(input);
  finish({ items: [{ id: "1", name: "普通组" }], total: 101 });
  await flushPromises();
  w.unmount();
});
