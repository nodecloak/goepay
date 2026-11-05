import { afterEach } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import type { Component } from "vue";

const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const wrapper of mounted.splice(0))
    if (wrapper.exists()) wrapper.unmount();
});

/** A browser reload creates a fresh component, keeping server state and session. */
export async function pageReload248<T extends VueWrapper>(
  wrapper: T,
): Promise<T> {
  const component = wrapper.vm.$options as Component;
  const props = wrapper.props();
  const router = (wrapper.vm as unknown as { $router?: any }).$router;
  const categoryControl = wrapper.find(
    '.settings-tabs > [id^="settings-tab-"][aria-selected="true"]',
  );
  const category = categoryControl.exists()
    ? categoryControl.attributes("id")
    : undefined;
  wrapper.unmount();
  const next = mount(component, {
    attachTo: document.body,
    props,
    global: router ? { plugins: [router] } : {},
  });
  mounted.push(next);
  await flushPromises();
  if (category && next.find(`#${category}`).exists()) {
    await next.get(`#${category}`).trigger("click");
    await flushPromises();
  }
  return next as T;
}
