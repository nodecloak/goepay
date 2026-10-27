// Synthetic inherited group DTO for unrelated group-editor contracts. The
// risk fields remain strict instead of making a successful GET return a list.
export function inheritedRiskGroup(groupID: string, groupName: string) {
  return {
    group_id: groupID,
    group_name: groupName,
    configured: false,
    revision: "0",
    fingerprint: "a".repeat(64),
    config: { pay_block_oversea: "", check_pay_regoin: "" },
    effective_oversea: "0",
    effective_scan_region: "0",
    geo_configured: false,
  };
}
