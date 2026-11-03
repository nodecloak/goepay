# Payment method marks

The console bundles these files locally; it does not load an icon CDN at runtime.
Brand marks identify the named payment methods. Their inclusion does not enable
a payment method, channel or adapter. Display names remain ordinary text.

## SVG sources

Retrieved on 2026-11-03:

| File | Source | License / changes |
| --- | --- | --- |
| `qqpay.svg` | [Remix Icon v4.6.0, qq-fill](https://github.com/Remix-Design/RemixIcon/blob/v4.6.0/icons/Logos/qq-fill.svg) | Apache-2.0; fixed fill to QQ blue `#12b7f5` for use as an image; added this modification notice. |
| `bank.svg` | [SVG Logos, unionpay](https://github.com/gilbarbara/logos/blob/22f03ffc4a652309d1fea4ade70f46e9e1d6602c/logos/unionpay.svg) | CC0-1.0; unchanged. |
| `paypal.svg` | [SVG Logos, paypal](https://github.com/gilbarbara/logos/blob/22f03ffc4a652309d1fea4ade70f46e9e1d6602c/logos/paypal.svg) | CC0-1.0; unchanged. |
| `douyinpay.svg` | [SVG Logos, tiktok-icon](https://github.com/gilbarbara/logos/blob/22f03ffc4a652309d1fea4ade70f46e9e1d6602c/logos/tiktok-icon.svg) | CC0-1.0; unchanged music-note brand mark shared by Douyin / TikTok. |
| `jdpay.svg` | GOEPAY vector drawing, keeping the existing JD letter mark and red tile appearance | Original geometric paths, covered by the project license; no embedded raster or font dependency. This is not an upstream official SVG download. |

The QQ path is copyright Remix Design. The accompanying license texts are
`RemixIcon-LICENSE.txt` and `SVGLogos-LICENSE.txt`. These licenses apply to those
assets; brand names and trademarks remain with their respective owners.

The production build also distributes these full license texts and attribution
at `/static/licenses/payment-methods/`, including `NOTICE.txt`.

The existing `alipay.png` and `wxpay.png` assets remain in use.
