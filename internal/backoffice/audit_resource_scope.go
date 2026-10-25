package backoffice

import "sort"

// AuditResourceScope describes registered metadata only. A historical audit
// snapshot may establish ownership; a mutable current record must not do so.
type AuditResourceScope struct {
	Name   string
	Title  string
	Tenant bool
}

func AuditResourceScopes() []AuditResourceScope {
	scopes := make([]AuditResourceScope, 0, len(definitions))
	for name, d := range definitions {
		scopes = append(scopes, AuditResourceScope{Name: name, Title: d.title, Tenant: d.tenant})
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Name < scopes[j].Name })
	return scopes
}
