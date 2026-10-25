package adminnotice

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminNotice77QQExpiredTokenRefreshesWithoutReplayingAnEarlierNotice(t *testing.T) {
	var tokens, messages atomic.Int32
	client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			tokens.Add(1)
			w.Write([]byte(`{"access_token":"short-lived77","expires_in":1}`))
			return
		}
		messages.Add(1)
		w.Write([]byte(`{"id":"local-expiry77"}`))
	}))
	s, _ := NewQQBot("local_app77", "synthetic77", client)
	if e := s.SendChannelStopped(context.Background(), "local_openid77", 7, 31, "SYSTEMERROR"); e != nil {
		t.Fatal(e)
	}
	if tokens.Load() != 1 || messages.Load() != 1 {
		t.Fatal("first notice duplicated")
	}
	time.Sleep(1100 * time.Millisecond)
	if e := s.SendChannelStopped(context.Background(), "local_openid77", 7, 32, "SYSTEMERROR"); e != nil {
		t.Fatal(e)
	}
	if tokens.Load() != 2 || messages.Load() != 2 {
		t.Fatal("expired token reused or previous notice repeated", tokens.Load(), messages.Load())
	}
}
