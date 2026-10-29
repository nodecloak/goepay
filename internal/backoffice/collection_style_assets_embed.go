package backoffice

import _ "embed"

// The three original pre_onecode styles (default / alipay / wxpay) shipped with
// site-local background images in epay_pro/public/static/files. GOEPAY has no
// such static path, so the bytes are embedded and seeded as uploaded
// collection-style assets on a fresh install (see collection_style_defaults.go).

//go:embed collectionassets/qrcode.png
var seedBackgroundDefault []byte

//go:embed collectionassets/qrcode_alipay.jpg
var seedBackgroundAlipay []byte

//go:embed collectionassets/qrcode_wxpay.png
var seedBackgroundWxpay []byte
