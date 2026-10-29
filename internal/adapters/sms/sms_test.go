package sms

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Unix(1700000000, 0).UTC() }

func TestAliyunSendSignsAndParsesOK(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.Write([]byte(`{"Code":"OK","Message":"OK","BizId":"1","RequestId":"x"}`))
	}))
	defer srv.Close()
	a := Aliyun{AccessKeyID: "AK", AccessKeySecret: "SK", SignName: "测试", TemplateCode: "SMS_1",
		BaseURL: srv.URL + "/", Now: fixedNow, Nonce: func() string { return "n123" }}
	if err := a.Send(context.Background(), "13800138000", "123456"); err != nil {
		t.Fatal(err)
	}
	if gotForm.Get("PhoneNumbers") != "13800138000" || gotForm.Get("Signature") == "" {
		t.Fatal("request not signed/addressed", gotForm)
	}
	if !strings.Contains(gotForm.Get("TemplateParam"), "123456") {
		t.Fatal("code not in TemplateParam")
	}
}

func TestAliyunSendRejectsAndUnknown(t *testing.T) {
	reject := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Code":"isv.MOBILE_NUMBER_ILLEGAL","Message":"no"}`))
	}))
	defer reject.Close()
	a := Aliyun{AccessKeyID: "AK", AccessKeySecret: "SK", SignName: "s", TemplateCode: "t", BaseURL: reject.URL + "/"}
	if err := a.Send(context.Background(), "1", "1"); err != ErrRejected {
		t.Fatal("want rejected", err)
	}
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer fail.Close()
	a.BaseURL = fail.URL + "/"
	if err := a.Send(context.Background(), "1", "1"); err != ErrUnknown {
		t.Fatal("want unknown on 500", err)
	}
	if err := (Aliyun{}).Send(context.Background(), "1", "1"); err != ErrConfig {
		t.Fatal("want config", err)
	}
}

func TestQcloudSendSignsAndParsesResult(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		if r.URL.Query().Get("sdkappid") != "app1" {
			t.Error("sdkappid missing")
		}
		w.Write([]byte(`{"result":0,"errmsg":"OK"}`))
	}))
	defer srv.Close()
	q := Qcloud{AppID: "app1", AppKey: "key", TplID: "42", Sign: "测试", BaseURL: srv.URL, Now: fixedNow, Random: func() int { return 555 }}
	if err := q.Send(context.Background(), "13800138000", "654321"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "654321") || !strings.Contains(gotBody, `"sig"`) || !strings.Contains(gotBody, "13800138000") {
		t.Fatal("body not formed", gotBody)
	}
}

func TestQcloudRejectsOnNonZeroResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":1024,"errmsg":"no"}`))
	}))
	defer srv.Close()
	q := Qcloud{AppID: "a", AppKey: "k", TplID: "1", Sign: "s", BaseURL: srv.URL}
	if err := q.Send(context.Background(), "1", "1"); err != ErrRejected {
		t.Fatal("want rejected", err)
	}
	if err := (Qcloud{}).Send(context.Background(), "1", "1"); err != ErrConfig {
		t.Fatal("want config", err)
	}
}

func TestSmsBaoSendMd5sPassAndReadsZero(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte("0"))
	}))
	defer srv.Close()
	s := SmsBao{User: "u", Pass: "secret", Template: "您的验证码是{code}", Sign: "测试", BaseURL: srv.URL}
	if err := s.Send(context.Background(), "13800138000", "112233"); err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("p") == "secret" || gotQuery.Get("p") != md5hex("secret") {
		t.Fatal("password not md5'd", gotQuery.Get("p"))
	}
	if !strings.Contains(gotQuery.Get("c"), "112233") || !strings.Contains(gotQuery.Get("c"), "测试") {
		t.Fatal("content not formed", gotQuery.Get("c"))
	}
}

func TestSmsBaoRejectsNonZeroAndUnknownOnEmpty(t *testing.T) {
	reject := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("30")) }))
	defer reject.Close()
	s := SmsBao{User: "u", Pass: "p", Template: "{code}", Sign: "s", BaseURL: reject.URL}
	if err := s.Send(context.Background(), "1", "1"); err != ErrRejected {
		t.Fatal("want rejected", err)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("")) }))
	defer empty.Close()
	s.BaseURL = empty.URL
	if err := s.Send(context.Background(), "1", "1"); err != ErrUnknown {
		t.Fatal("want unknown", err)
	}
	if err := (SmsBao{}).Send(context.Background(), "1", "1"); err != ErrConfig {
		t.Fatal("want config", err)
	}
}
