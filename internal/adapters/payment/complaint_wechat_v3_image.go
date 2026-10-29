package payment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// UploadImage uploads a complaint image to WeChat V3 and returns its media_id,
// mirroring the original ComplainService::uploadImage. WeChat's media upload is
// a multipart POST whose Authorization signature is computed over a `meta` JSON
// ({filename, sha256}) as the request body, NOT over the multipart payload, so
// it cannot go through wechatV3Client.call (JSON only) — it is signed and
// verified here with the same SignWechatV3Request + VerifyWechatV3Message
// helpers, introducing no new signing path. A transport failure or any
// non-2xx/unverified response is an *UnknownOutcome: a caller must not assume a
// media id was created.
const wechatComplaintImagePath = "/v3/merchant/media/upload"

var wechatImageContentTypes = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"bmp":  "image/bmp",
}

func validImageFilename(name string) (string, bool) {
	if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n\"\\/") {
		return "", false
	}
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 {
		return "", false
	}
	ext := strings.ToLower(name[dot+1:])
	ct, ok := wechatImageContentTypes[ext]
	return ct, ok
}

func (w *WechatV3Complaint) UploadImage(ctx context.Context, filename string, data []byte) (string, error) {
	contentType, ok := validImageFilename(filename)
	if !ok || len(data) == 0 || len(data) > 2<<20 {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(data)
	meta, e := json.Marshal(map[string]string{"filename": filename, "sha256": hex.EncodeToString(sum[:])})
	if e != nil {
		return "", ErrInvalid
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if e = mw.WriteField("meta", string(meta)); e != nil {
		return "", ErrInvalid
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", contentType)
	part, e := mw.CreatePart(header)
	if e != nil {
		return "", ErrInvalid
	}
	if _, e = part.Write(data); e != nil {
		return "", ErrInvalid
	}
	if e = mw.Close(); e != nil {
		return "", ErrInvalid
	}

	n, e := nonce()
	if e != nil {
		return "", e
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	// WeChat signs the media upload over the meta JSON, not the multipart body.
	signature, e := SignWechatV3Request("POST", wechatComplaintImagePath, stamp, n, meta, w.client.config.PrivateKey)
	if e != nil {
		return "", e
	}
	request, e := http.NewRequestWithContext(ctx, "POST", "https://api.mch.weixin.qq.com"+wechatComplaintImagePath, bytes.NewReader(buf.Bytes()))
	if e != nil {
		return "", ErrInvalid
	}
	request.Header.Set("Authorization", fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`, w.client.config.MerchantID, n, stamp, w.client.config.MerchantSerial, signature))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", mw.FormDataContentType())
	request.Header.Set("User-Agent", "GOEPAY/1")

	response, e := w.client.config.HTTPClient.Do(request)
	if e != nil {
		return "", &UnknownOutcome{Operation: wechatComplaintImagePath, Cause: ErrAuthentication}
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return "", &UnknownOutcome{Operation: wechatComplaintImagePath, Cause: ErrAuthentication}
	}
	if e = VerifyWechatV3Message(response.Header, raw, w.client.config.VerificationKeys, time.Now(), 5*time.Minute); e != nil {
		return "", &UnknownOutcome{Operation: wechatComplaintImagePath, Cause: ErrAuthentication}
	}
	if content, _, ce := mime.ParseMediaType(response.Header.Get("Content-Type")); ce != nil || content != "application/json" {
		return "", &UnknownOutcome{Operation: wechatComplaintImagePath, Cause: ErrAuthentication}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", &UnknownOutcome{Operation: wechatComplaintImagePath, Cause: ErrAuthentication}
	}
	out, e := v3Object(raw)
	if e != nil {
		return "", &UnknownOutcome{Operation: wechatComplaintImagePath, Cause: ErrAuthentication}
	}
	id := v3String(out, "media_id")
	if id == "" || len(id) > 512 {
		return "", &UnknownOutcome{Operation: wechatComplaintImagePath, Cause: ErrAuthentication}
	}
	return id, nil
}
