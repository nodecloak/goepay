package adminnotice

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAdminNotice77QQConcurrentDistinctNoticesShareOnlyUnexpiredProfileToken(t *testing.T) {
	var tokens, messages atomic.Int32
	client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			tokens.Add(1)
			w.Write([]byte(`{"access_token":"local-token77","expires_in":"7200"}`))
			return
		}
		messages.Add(1)
		if r.Header.Get("Authorization") != "QQBot local-token77" {
			t.Error("wrong cached profile token")
		}
		w.Write([]byte(`{"id":"local-message77"}`))
	}))
	s, e := NewQQBot("local_app77", "synthetic77", client)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := int64(1); i <= 8; i++ {
		wg.Add(1)
		go func(event int64) {
			defer wg.Done()
			if e := s.SendChannelStopped(context.Background(), "local_openid77", 7, event, "SYSTEMERROR"); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if tokens.Load() != 1 || messages.Load() != 8 {
		t.Fatal("profile token fetched repeatedly or notice retried", tokens.Load(), messages.Load())
	}
	other, e := NewQQBot("local_app77", "changed-synthetic77", client)
	if e != nil {
		t.Fatal(e)
	}
	if e = other.SendChannelStopped(context.Background(), "local_openid77", 7, 9, "SYSTEMERROR"); e != nil || tokens.Load() != 2 || messages.Load() != 9 {
		t.Fatal("token crossed profile boundary", e, tokens.Load(), messages.Load())
	}
}

func TestAdminNotice77QQRejectsInvalidTokenLifetimeAndContradictoryErrorBeforeAcceptance(t *testing.T) {
	for _, body := range []string{
		`{"access_token":"local-token77","expires_in":0}`,
		`{"access_token":"local-token77","expires_in":"7200.5"}`,
		`{"access_token":"local-token77","expires_in":9999999999999}`,
		`{"access_token":"local-token77","expires_in":7200,"code":400}`,
	} {
		var calls atomic.Int32
		client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.URL.Path == "/app/getAppAccessToken" {
				w.Write([]byte(body))
				return
			}
			w.Write([]byte(`{"id":"must-not-send77"}`))
		}))
		s, _ := NewQQBot("app77", "synthetic77", client)
		if e := s.SendChannelStopped(context.Background(), "local_openid77", 7, 31, "SYSTEMERROR"); e != ErrDelivery || calls.Load() != 1 {
			t.Fatal("unsafe credentials caused message IO", e, calls.Load())
		}
	}
	client, _ := noticeTLS77(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/getAppAccessToken" {
			w.Write([]byte(`{"access_token":"local-token77","expires_in":7200}`))
			return
		}
		w.Write([]byte(`{"id":"contradictory77","code":403}`))
	}))
	s, _ := NewQQBot("app77", "synthetic77", client)
	if e := s.SendChannelStopped(context.Background(), "local_openid77", 7, 31, "SYSTEMERROR"); e != ErrDelivery {
		t.Fatal("error with message ID accepted", e)
	}
}
