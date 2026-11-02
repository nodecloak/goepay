# Certification/OCR provider references, 2026-11-02

This review used public documentation and the ignored local PHP/JavaScript references. No production credentials, identity submissions, document uploads or billable identity API tests were used. Provider tests use local TLS peers only. A passing local test is not live provider acceptance or proof that an account has the required product entitlement.

## Protocols and current differences

| Mode | Implemented protocol and proof | Reference and limitation |
|---|---|---|
| 1, Alipay identity | Fixed `https://openapi.alipay.com/gateway.do`; signed `alipay.user.certify.open.initialize`, page launch `alipay.user.certify.open.certify`, verified `alipay.user.certify.open.query`. `SMART_FACE`; six original document types. Only verified `passed=T` succeeds. `F` stays pending conservatively. | Local `epay_pro/vendor/cccyun/alipay-sdk/src/AlipayCertifyService.php`; [official service reference](https://opendocs.alipay.com/open/repo-013ubq), [initialize reference](https://opendocs.alipay.com/open/02ahjy). The official site fetch timed out during this review; no current onboarding or price assertion is made. Existing Go AOP signer and response/certificate verifier are reused for both public-key and certificate account modes. |
| 2, bound phone | Fixed HTTPS GET `phone3.market.alicloudapi.com/phonethree`, `Authorization: APPCODE`, query `idcard,realname,phone`. Original response `code=200` is success; other/unknown responses cannot succeed. Phone is server-owned bound phone. | [Original cloud-market product](https://market.aliyun.com/detail/cmapi031847). Its search index shows HTTPS, but the current document fetch renders only navigation; original parameter/response semantics are retained from plaintext `epay_release/includes/functions.php`. No HTTP fallback or live endpoint request was made. |
| 3, Alipay certificate comparison | Verified `alipay.user.certdoc.certverify.preconsult`, fixed `openauth.alipay.com/oauth2/publicAppAuthorize.htm` with `scope=id_verify`, exact signed top-level code exchange via `alipay.system.oauth.token`, then verified `alipay.user.certdoc.certverify.consult` with signed top-level `auth_token`. `T` passes, `F` rejects. | Local `AlipayCertdocService.php`; [official product reference](https://opendocs.alipay.com/open/repo-00hddl). The domain must consume the state-bound authorization-code claim before I/O; an uncertain exchange must never automatically reuse the code. |
| 4, Tencent | Modern TC3-HMAC-SHA256 `DetectAuth` and `GetDetectInfoEnhanced`, fixed `faceid.tencentcloudapi.com`, version `2018-03-01`, no Region required. Mandatory `RuleId` is `cert_qcloudruleid`. Initialization sends `RuleId,Name,IdCard,RedirectUrl,Extra`; returns `BizToken,Url`. Query asks only `InfoType=1`. `Text=null` is pending; explicit `Text.ErrCode=0` plus matching returned Name/IdCard is required to pass; nonzero rejects. | [DetectAuth](https://cloud.tencent.com/document/product/1007/31816), [GetDetectInfoEnhanced](https://cloud.tencent.com/document/product/1007/41957), [FAQ](https://cloud.tencent.cn/document/faq/1007/45894). [Official history](https://cloud.tencent.com/document/api/1007/31327) records removal of old `GetRealNameAuthToken/Result` in January 2023, so they are deliberately not copied from PHP. RuleIds requiring intention verification are unavailable here; face success alone is insufficient for that workflow. |
| 5, Alibaba financial-grade | HTTPS POST `saf.cn-shanghai.aliyuncs.com`, RPC `ExecuteRequest`, Service `fin_face_verify`, version `2017-03-31`, HMAC-SHA1. H5 initialization uses current `bizCode=FACE` rather than the PHP native-SDK `FACE_SDK`; returns `certifyId,certifyUrl`. Accepts documented upper/lower response wrapper naming. | [Current native HTTPS documentation](https://help.aliyun.com/zh/id-verification/financial-grade-id-verification/native-https-calls-1). `passed=F` means failure **or not submitted**, so the adapter preserves pending; only `T` succeeds. Initialization URLs/IDs are documented as 30-minute, one-submission material; no live entitlement or pricing validation was performed. |
| Corporate precheck | Fixed HTTPS GET `companythree.shumaidata.com/companythree/check`; APPCODE; query `companyName,creditNo,legalPerson`; requires `code=200` and `data.result=0` before starting the personal flow. | Plaintext local reference. The [original cloud-market product](https://market.aliyun.com/products/56928005/cmapi00043309.html) could not be fetched and a primary current HTTPS protocol document was not found. HTTPS-only adapter and local parity tests exist; operational compatibility remains unverified. It never falls back to the original plaintext HTTP endpoint. |

Mode 6 remains unavailable. The newer local Pro labels it an extension, whereas the older release/original live settings label it Ant Digital. Neither an installed extension nor a verified common protocol was available.

## Provider launch URL boundaries

All accepted launch URLs use HTTPS, exact hosts, no userinfo, no explicit ports, no backslashes/control characters and only these paths/fragments:

| Mode | Host | Path | Fragment |
|---|---|---|---|
| 1 | `openapi.alipay.com` | `/gateway.do` | empty |
| 3 | `openauth.alipay.com` | `/oauth2/publicAppAuthorize.htm` | empty |
| 4 | `faceid.qq.com` | empty or `/` | empty |
| 4 | `open.weixin.qq.com` | `/connect/oauth2/authorize` | empty or `wechat_redirect` |
| 5 | `picker.antcloudauth.aliyuncs.com` | `/gateway.do` | empty |

Alipay URLs are constructed locally from signed/verified identifiers. Other URLs must arrive through the authenticated fixed provider TLS request. Browser callbacks are navigation signals, never result proof. Tencent's private opaque token packages the provider token, attempt ID and hashes of the original identity; the domain encrypts it and never exposes it to browsers.

## OCR

Alibaba uses fixed `ocr-api.cn-hangzhou.aliyuncs.com`, version `2021-07-07`, RPC signing and binary uploaded bytes. Its `Data` is a JSON string; ID sides use `data.face.data` / `data.back.data`, other documents use `data`. [Official API overview](https://help.aliyun.com/en/ocr/developer-reference/api-ocr-api-2021-07-07-overview), [ID card API](https://help.aliyun.com/zh/ocr/developer-reference/api-ocr-api-2021-07-07-recognizeidcard), [enterprise credential APIs](https://help.aliyun.com/zh/ocr/developer-reference/api-ocr-api-2021-07-07-dir-enterprise-qualification-identification/).

Baidu uses fixed `aip.baidubce.com`, POST `/oauth/2.0/token` with client credentials, then uploaded image bytes encoded as base64 in form field `image`. There is no automatic OCR retry or constructor token request. [Current bank-card protocol](https://ai.baidu.com/ai-doc/OCR/ak3h7xxg3), [account-opening protocol](https://ai.baidu.com/ai-doc/OCR/7lsb7obsj). The adapter uses a conservative 2 MiB raw and 4 MiB base64-plus-form-escaped image limit, covering the tighter document API limit even though bank-card currently permits more.

| Document enum | Alibaba action | Baidu `/rest/2.0/ocr/v1/` suffix | Normalized fields |
|---|---|---|---|
| `idcard` | `RecognizeIdcard` | `idcard`, side front | `id_no,name,address,sex,ethnicity,birth_date` |
| `idcard_back` | `RecognizeIdcard` | `idcard`, side back | `issue_authority,period_begin,period_end` |
| `passport` | `RecognizePassport` | `overseas_passport` | `passport_no,name,sex,country,birth_date,period_end`; Alibaba also `passport_type,last_name,first_name,period_begin` |
| `bank_card` | `RecognizeBankCard` | `bankcard` | `bank_name,card_type,card_no,period_end` |
| `business_license` | `RecognizeBusinessLicense` | `business_license` | `license_no,name,address,reg_date,legal_name,type,registered_capital,business_scope,period_begin,period_end` |
| `bank_account_license` | `RecognizeBankAccountLicense` | `account_opening` | `bank_account,legal_name,bank_name,approval_no,name,permit_no` |

OCR only produces reviewable fields. It accepts neither remote image URLs nor server filesystem paths and never certifies a merchant.

## Local verification scope

Focused tests cover all twelve OCR document/provider combinations; Tencent TC3 request signatures, pending/rejected/missing-proof/changed-identity cases; Alibaba RPC request signatures and pending/passed; phone/company exact requests; Alipay signed initialization/query/code exchange/consult in key and certificate modes; redacted errors; malformed, duplicated and oversized JSON; missing verified TLS; redirects; lookalike launch hosts; no invalid-input I/O; and encoded image limits.

Final command and logs are recorded by the provider agent in `.tools/kyc233-provider-final-20261102/`. Tests use deterministic synthetic identities and local provider peers, not live provider acceptance.
