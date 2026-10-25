package backoffice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
)

func init() {
	d := definitions["merchants"]
	d.fields = append(d.fields, Field{Name: "confirm_group_assignment", Label: "明确确认基础组（需同时提交用户组 ID）", Type: "boolean", WriteOnly: true})
	definitions["merchants"] = d
}

func groupAssignmentAudit(v, current map[string]any, raw map[string]json.RawMessage) (map[string]any, bool, error) {
	changes := map[string]any{}
	for k, x := range v {
		if k != "confirm_group_assignment" && k != "group_assignment" {
			changes[k] = x
		}
	}
	value := func(x any) string {
		if x == nil {
			return ""
		}
		return fmt.Sprint(x)
	}
	assigned, previous := value(v["group_id"]), value(current["group_id"])
	confirmed, _ := v["confirm_group_assignment"].(bool)
	if confirmed {
		if _, ok := raw["group_id"]; !ok {
			return nil, false, ErrInvalid
		}
	}
	if assigned != previous || confirmed {
		source := "changed"
		if confirmed {
			source = "confirmed"
		}
		changes["group_assignment"] = map[string]any{"version": 1, "source": source, "previous": previous, "assigned": assigned}
	}
	return changes, confirmed, nil
}
func confirmGroupAssignment(ctx context.Context, tx pgx.Tx, id int64, profile map[string]any) error {
	group := int64(0)
	if raw := profile["group_id"]; raw != nil && raw != "" {
		var e error
		group, e = strconv.ParseInt(fmt.Sprint(raw), 10, 64)
		if e != nil || group < 0 {
			return ErrInvalid
		}
	}
	_, e := tx.Exec(ctx, "UPDATE merchants SET base_group_id=$2,group_baseline_unresolved=false WHERE id=$1", id, group)
	return e
}
