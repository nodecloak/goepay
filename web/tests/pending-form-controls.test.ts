import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import KitSelect from "../src/components/KitSelect.vue";
import ResourcePage from "../src/components/ResourcePage.vue";
import { ApiClient, ApiError } from "../src/api";

const wrappers: VueWrapper[] = [];
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount());
  vi.restoreAllMocks();
});

it("closes an open select when a pending action disables it, without accepting its old options", async () => {
  const wrapper = mount(KitSelect, {
    props: { modelValue: "a", options: ["a", "b"], disabled: false },
  });
  wrappers.push(wrapper);
  await wrapper.get(".pa-select-trigger").trigger("click");
  const option = wrapper.findAll('[role="option"]')[1]!;
  await wrapper.setProps({ disabled: true });
  option.element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  expect(wrapper.emitted("change")).toBeUndefined();
  expect(wrapper.find('[role="listbox"]').exists()).toBe(false);
  await wrapper.setProps({ disabled: false });
  await wrapper.get(".pa-select-trigger").trigger("click");
  await wrapper.findAll('[role="option"]')[1]!.trigger("click");
  expect(wrapper.emitted("update:modelValue")).toEqual([["b"]]);
});

it("locks generic resource fields during a slow save and restores the draft after a rejection", async () => {
  const client = new ApiClient("admin");
  let rejectWrite!: (reason: Error) => void;
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path, options) => {
      if (options?.method === "POST")
        return new Promise((_, reject) => {
          rejectWrite = reject;
        });
      if (path.endsWith("/schema"))
        return {
          title: "授权域名",
          permissions: { create: true, update: true, delete: true },
          fields: [
            { name: "domain", label: "域名", type: "string", required: true },
          ],
        } as never;
      return { items: [], total: 0 } as never;
    });
  const wrapper = mount(ResourcePage, {
    props: { client, resource: "domains" },
  });
  wrappers.push(wrapper);
  await flushPromises();
  await wrapper
    .findAll("button")
    .find((button) => button.text().includes("新增"))!
    .trigger("click");
  const field = wrapper.get<HTMLInputElement>("#field-domain");
  await field.setValue("shop.example.test");
  await wrapper.get(".resource-modal form").trigger("submit");
  await flushPromises();
  expect(field.element.disabled).toBe(true);
  expect(request.mock.calls.at(-1)![1]?.body).toEqual({
    domain: "shop.example.test",
  });
  rejectWrite(new ApiError("域名已存在", 422, -1));
  await flushPromises();
  expect(field.element.disabled).toBe(false);
  expect(field.element.value).toBe("shop.example.test");
});
