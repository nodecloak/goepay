// App.vue loads console pages on demand. Tests that mount the whole app import
// those page modules first, so a lazily rendered page is ready after the usual
// flushPromises() instead of racing a dynamic import. Only the pages App.vue
// actually loads lazily are preloaded, read from its source to stay in sync.
import appSource from "../src/App.vue?raw";

const components = import.meta.glob("../src/components/*.vue");
const lazyPages = [
  ...appSource.matchAll(
    /defineAsyncComponent\(\s*\(\)\s*=>\s*import\("\.\/components\/([A-Za-z0-9]+\.vue)"\)/g,
  ),
].map((match) => `../src/components/${match[1]}`);

export async function preloadAppPages() {
  if (lazyPages.length === 0)
    throw new Error("App.vue lazy page list could not be read");
  await Promise.all(
    lazyPages.map((page) => {
      const load = components[page];
      if (!load) throw new Error(`missing lazy page ${page}`);
      return load();
    }),
  );
}
