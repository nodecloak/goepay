import { ref, watch, type Ref } from "vue";
import type { ApiClient } from "./api";
import { FILTER_PAY_TYPES, payTypeLabel } from "./pay-types";

// A pay-type option shown in a 支付方式 dropdown: the type code and its
// showname. The leading "all" row is added by each caller because its label
// differs (全部方式 on the list filters, 所有支付方式 on the statistics pages).
export interface PayTypeOption {
  value: string;
  label: string;
}

// The original console builds every 支付方式 dropdown from the live pay-type /
// channel list (value = type, label = showname) rather than a fixed set. This
// composable mirrors that: it reads the read-only /pay-type-options endpoint
// (the enabled CatalogPayType set the router already accepts) and exposes the
// options reactively. The result is cached per ApiClient so the many filter and
// statistics pages that share a console session fetch it once.
//
// On any failure it falls back to the static catalog superset so a dropdown is
// never empty; the backend still validates the submitted type, so an offered
// value the server rejects simply returns no rows.
const cache = new WeakMap<ApiClient, Ref<PayTypeOption[]>>();

function fallbackOptions(): PayTypeOption[] {
  return FILTER_PAY_TYPES.map((value) => ({
    value,
    label: payTypeLabel(value),
  }));
}

function sanitize(data: unknown): PayTypeOption[] {
  const items =
    data && typeof data === "object" && Array.isArray((data as any).items)
      ? ((data as any).items as unknown[])
      : [];
  const seen = new Set<string>();
  const options: PayTypeOption[] = [];
  for (const raw of items) {
    if (!raw || typeof raw !== "object") continue;
    const value = (raw as any).value;
    const showname = (raw as any).showname;
    if (typeof value !== "string" || !/^[a-z][a-z0-9_]{0,31}$/.test(value))
      continue;
    if (seen.has(value)) continue;
    seen.add(value);
    const label =
      typeof showname === "string" && showname.trim()
        ? showname
        : payTypeLabel(value);
    options.push({ value, label });
  }
  return options.length ? options : fallbackOptions();
}

export function usePayTypeOptions(
  clientRef: Ref<ApiClient>,
  enabled?: Ref<boolean>,
): Ref<PayTypeOption[]> {
  const options = ref<PayTypeOption[]>(fallbackOptions());
  let epoch = 0;
  async function load() {
    if (enabled && !enabled.value) return;
    const client = clientRef.value;
    const cached = cache.get(client);
    if (cached) {
      options.value = cached.value;
      // Keep this ref in sync with later cache refreshes for the same client.
      watch(cached, (next) => (options.value = next));
      return;
    }
    const shared = ref<PayTypeOption[]>(fallbackOptions());
    cache.set(client, shared);
    options.value = shared.value;
    const localEpoch = ++epoch;
    try {
      const data = await client.request<unknown>("/pay-type-options");
      if (localEpoch !== epoch || clientRef.value !== client) return;
      const next = sanitize(data);
      shared.value = next;
      options.value = next;
    } catch {
      // keep the fallback already in place
    }
  }
  watch(clientRef, load, { immediate: true });
  if (enabled) watch(enabled, (on) => on && load());
  return options;
}
