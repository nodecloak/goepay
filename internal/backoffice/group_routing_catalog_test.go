package backoffice

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestGroupRoutingCatalogReturnsOnlyActualRuntimeStringsAndEnforcesRole(t *testing.T) {
	s := New(nil, nil)
	s.GroupRoutingChannels = func() ([]GroupPaymentChannel, error) {
		return []GroupPaymentChannel{{ID: "9007199254740993", Type: "alipay", Methods: []string{"native", "page"}}}, nil
	}
	request := func(role, method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Serve(w, httptest.NewRequest(method, path, nil), Principal{Role: role, SubjectID: 1, AdminClass: "super"}, []string{"groups", "routing-options"})
		return w
	}
	w := request("admin", "GET", "/groups/routing-options")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Items     []GroupPaymentChannel `json:"items"`
			Available bool                  `json:"available"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil || len(body.Data.Items) != 1 || body.Data.Items[0].ID != "9007199254740993" || !body.Data.Available {
		t.Fatal(body, e)
	}
	if w = request("merchant", "GET", "/groups/routing-options"); w.Code != 403 {
		t.Fatal("merchant catalog access", w.Code)
	}
	if w = request("admin", "POST", "/groups/routing-options"); w.Code != 405 {
		t.Fatal("write accepted", w.Code)
	}
	if w = request("admin", "GET", "/groups/routing-options?private_key=1"); w.Code != 400 {
		t.Fatal("unknown filter accepted", w.Code)
	}
}
