package payment

// Port of epay_release/plugins/yinyingtong (银盈通支付) — INTENTIONALLY UNSUPPORTED.
//
// The create paths (PayClient prepay/jsapipay/precreate) use a plain MD5
// signature and would port cleanly. The blocker is the PAYMENT NOTIFICATION,
// which this platform cannot verify correctly from the available SDK:
//
//   PayClient::notify($data, $key) does:
//       $data   = trim(substr($data, 107));   // drop a 107-byte header
//       $len    = trim(substr($data, 0, 4));
//       $cipher = trim(substr($data, 4));
//       $dec    = desDecrypt($cipher, $key);   // 3DES-ECB, hex input
//       $json   = explode("\x..", $dec)[1];    // split on a control byte
//
//   The 107-byte header layout and the exact control-byte separator inside the
//   decrypted blob are undocumented and not derivable from the SDK, and there
//   is no signature over the result — authenticity would rest entirely on the
//   product key decrypting to well-formed JSON. A payment plugin whose success
//   notification cannot be verified to a standard is unsafe to ship, so per the
//   brief we leave Build returning ErrUnsupported rather than port a callback
//   we cannot test against a known vector.
//
//   The 快捷 (quick) path (M2Client) additionally needs: a per-merchant RSA
//   private key read from a <appid>.pfx PKCS#12 file (golang.org/x/crypto/pkcs12
//   is available), RC4 key wrapping, and SM4-ECB — none of which help without a
//   verifiable notification, and the PFX itself is a filesystem artifact the
//   Build contract forbids touching.
//
// The spec is still registered so the channel appears in the console and the
// reason is explicit; BuildPluginChannel returns ErrUnsupported.

func init() {
	RegisterPlugin(PluginSpec{
		Name:   "yinyingtong",
		Title:  "银盈通支付",
		Author: "银盈通",
		Link:   "http://www.yinyingtong.com/",
		Types:  []string{"alipay", "wxpay", "bank"},
		Inputs: []PluginInput{
			{Key: "appid", Label: "应用ID", Required: true},
			{Key: "appkey", Label: "应用KEY", Note: "同时是私钥证书密码", Secret: true, Required: true},
			{Key: "productkey", Label: "产品密钥", Note: "用于支付回调数据解密，填错将无法回调", Secret: true},
			{Key: "appmchid", Label: "交易商户企业号"},
			{Key: "trade_platform_no", Label: "平台商企业号(参考号)", Note: "仅分账需要填写"},
			{Key: "terminal_number", Label: "终端号", Note: "仅快捷支付填写"},
			{Key: "channel_merch_no", Label: "渠道商户号", Note: "可留空，多个渠道商户号可用,分隔"},
		},
		Note:  "回调解密格式无法独立复现，暂不支持 (notification format not reproducible/verifiable)",
		Build: newYinyingtongPlugin,
	})
}

// newYinyingtongPlugin refuses to build: the payment notification cannot be
// verified correctly from the available SDK (see the file comment).
func newYinyingtongPlugin(PluginChannel) (Provider, error) {
	return nil, ErrUnsupported
}
