package payment

import "testing"

func TestProviderOriginLocalSimulationBoundary(t *testing.T) {
	for _, tc := range []struct {
		url              string
		simulation, want bool
	}{{"https://pay.example.com", false, true}, {"http://127.0.0.1:8180", false, false}, {"http://127.0.0.1:8180", true, true}, {"http://pay.example.com", true, false}, {"http://169.254.169.254", true, false}, {"https://pay.example.com?x=1", false, false}} {
		if got := validProviderOrigin(tc.url, tc.simulation); got != tc.want {
			t.Fatalf("%s simulation%v got%v", tc.url, tc.simulation, got)
		}
	}
}
