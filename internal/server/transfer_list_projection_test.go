package server

import "testing"

func TestTransferListProjectionAlwaysRemovesPrivateColumnsAndRejectsMalformedRows(t *testing.T) {
	columns := []string{"id", "amount", "type", "account_masked", "name_masked", privateTransferRecipientColumn}
	values := []any{"1", "100", "", "", "", nil}
	s := &Server{}
	got, e := s.projectTransferListValues(columns, values)
	if e != nil || len(got) != 5 || len(transferPublicColumns(columns)) != 5 {
		t.Fatal("legacy projection cannot be read safely", e)
	}
	for _, v := range []struct {
		columns []string
		values  []any
	}{
		{columns, values[:5]},
		{columns[:5], values[:5]},
		{[]string{"id", "id", "type", "account_masked", "name_masked", privateTransferRecipientColumn}, values},
		{columns, []any{1, "100", "", "", "", nil}},
		{columns, []any{"01", "100", "", "", "", nil}},
		{columns, []any{"1", "0", "", "", "", nil}},
		{columns, []any{"1", "100", "", "", "", "private-cipher-must-not-be-serialized"}},
		{columns, []any{"1", "100", "", "", "", []byte("bad authentication")}},
	} {
		if _, e = s.projectTransferListValues(v.columns, v.values); e == nil {
			t.Fatal("invalid private projection accepted")
		}
	}
}
