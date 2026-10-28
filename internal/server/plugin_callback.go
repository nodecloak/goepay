package server

// callbackMethodAllowed reports whether a provider accepts upstream callbacks
// with the given HTTP method. Providers that do not say keep the original
// POST-only contract.
func callbackMethodAllowed(p any, method string) bool {
	m, ok := p.(interface{ NotificationMethods() []string })
	if !ok || m.NotificationMethods() == nil {
		return method == "POST"
	}
	for _, v := range m.NotificationMethods() {
		if v == method {
			return true
		}
	}
	return false
}
