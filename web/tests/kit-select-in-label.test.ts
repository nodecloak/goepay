import { mount } from "@vue/test-utils";
import { expect, it } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";
import KitField from "../src/components/KitField.vue";
import KitSelect from "../src/components/KitSelect.vue";

// KitField is a <label>. Choosing an option removes the menu, so the label
// used to pass the same click on to its first control, the select's trigger,
// and the menu opened again over the fields below.
it("a select inside a field label stays closed after an option is chosen", async () => {
  const value = ref("");
  const Form = defineComponent(
    () => () =>
      h(KitField, { label: "状态筛选" }, () =>
        h(KitSelect, {
          modelValue: value.value,
          "onUpdate:modelValue": (v: string) => (value.value = v),
          options: [
            { value: "", label: "全部状态" },
            { value: "paid", label: "已支付" },
          ],
          ariaLabel: "状态筛选",
        }),
      ),
  );
  const w = mount(Form, { attachTo: document.body });
  (w.get(".pa-select-trigger").element as HTMLButtonElement).click();
  await nextTick();
  const option = w
    .findAll('[role="option"]')
    .find((o) => o.text() === "已支付")!;
  // A browser runs the label's own click only when the event is not
  // cancelled; jsdom does not model that, so check the cancel itself.
  const click = new MouseEvent("click", { bubbles: true, cancelable: true });
  option.element.dispatchEvent(click);
  expect(click.defaultPrevented).toBe(true);
  await nextTick();
  await nextTick();
  expect(value.value).toBe("paid");
  expect(w.find('[role="listbox"]').exists()).toBe(false);
  expect(w.get(".pa-select-trigger").attributes("aria-expanded")).toBe("false");
  w.unmount();
});
