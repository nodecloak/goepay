import { toRaw } from "vue";
import type { ApiClient } from "./api";

// Only unresolved dialog state is retained, in memory, within the current
// ApiClient session. Switching credentials or area cannot recover another
// operator's pending confirmation. No credentials are written to Web Storage.
const sessions = new WeakMap<
  ApiClient,
  {
    proof: string;
    entries: Map<string, unknown>;
  }
>();

export function orderOperationMemory212<T>(
  client: ApiClient,
  kind: "income" | "notify",
  tradeNo: string,
) {
  const owner = toRaw(client),
    proof = owner.csrfToken;
  let session = sessions.get(owner);
  if (!session || session.proof !== proof) {
    session = { proof, entries: new Map() };
    sessions.set(owner, session);
  }
  const entries = session.entries,
    key = kind + "/" + tradeNo;
  return {
    read: () => entries.get(key) as T | undefined,
    save: (state: T) => {
      if (owner.csrfToken === proof && sessions.get(owner)?.entries === entries)
        entries.set(key, state);
    },
    clear: () => entries.delete(key),
  };
}
