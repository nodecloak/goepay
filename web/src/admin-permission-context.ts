import { ref } from "vue";
import type { PermissionCatalogEntry } from "./components/AdminPermissionField.vue";

// Session-scoped admin permission context (migration 000132). App.vue sets these
// from the session payload so the admin-user editor's permission_tree field can
// render the grantable catalog and hide itself for a super administrator,
// without threading props through the shared resource form.
export const adminPermissionCatalog = ref<PermissionCatalogEntry[]>([]);

export function setAdminPermissionCatalog(
  catalog: PermissionCatalogEntry[] | undefined,
) {
  adminPermissionCatalog.value = Array.isArray(catalog) ? catalog : [];
}
