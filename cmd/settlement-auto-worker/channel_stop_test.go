package main

import "testing"

func TestChannelStop76ExecutorExplicitCanonicalOptIn(t *testing.T) {
	for _, flag := range []string{"", "false"} {
		enabled, e := channelAutoStopSetting(flag)
		if e != nil || enabled {
			t.Fatal("automatic channel stop default changed", flag, e)
		}
	}
	enabled, e := channelAutoStopSetting("true")
	if e != nil || !enabled {
		t.Fatal("explicit automatic channel stop unavailable", e)
	}
	for _, flag := range []string{"TRUE", "1", " true", "yes"} {
		if _, e = channelAutoStopSetting(flag); e == nil {
			t.Fatal("ambiguous automatic channel stop accepted", flag)
		}
	}
}
