import type { ApiClient } from "./api";
import type { Area } from "./menu";

interface Identity {
  client: ApiClient;
  csrf: string;
  area: Area;
}
export interface SharingRequestContext extends Identity {
  version: number;
}

// Detect direct mutations as well as reactive prop changes. An old response may
// invalidate its own generation, but can never clear a newer session's data.
export function sharingRequestContext(
  read: () => Identity,
  invalidate: () => void,
) {
  let identity = read(),
    version = 0,
    alive = true;
  const same = (a: Identity, b: Identity) =>
    a.client === b.client && a.csrf === b.csrf && a.area === b.area;
  function refresh() {
    const next = read();
    if (!same(identity, next)) {
      identity = next;
      ++version;
      invalidate();
    }
  }
  return {
    capture(): SharingRequestContext {
      refresh();
      return { ...identity, version };
    },
    current(value: SharingRequestContext): boolean {
      if (!alive || value.version !== version) return false;
      refresh();
      return value.version === version && same(value, identity);
    },
    dispose() {
      alive = false;
      ++version;
    },
  };
}
