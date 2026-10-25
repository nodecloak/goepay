package payment

import "strings"

type transferFailureClass uint8

const (
	transferFailureIgnore76 transferFailureClass = iota
	transferFailurePayee76
	transferFailureChannel76
)

// Business-code classification is only meaningful after the adapter's original
// submit receipt has been authenticated. It never decides transfer finality or
// changes the existing definite-rejection policy for releasing a reserve.
func classifyTransferFailure76(code string) transferFailureClass {
	if len(code) == 0 || len(code) > 128 {
		return transferFailureIgnore76
	}
	value := code
	if i := strings.IndexByte(code, ':'); i >= 0 {
		if i != 5 || code[:5] == "10000" || code[0] < '1' || code[0] > '9' {
			return transferFailureIgnore76
		}
		for j := 0; j < 5; j++ {
			if code[j] < '0' || code[j] > '9' {
				return transferFailureIgnore76
			}
		}
		value = code[i+1:]
	}
	if value == "" || !failureCodeLetter(value[0]) {
		return transferFailureIgnore76
	}
	for i := 1; i < len(value); i++ {
		b := value[i]
		if !failureCodeLetter(b) && (b < '0' || b > '9') && b != '_' && b != '.' && b != '-' {
			return transferFailureIgnore76
		}
	}
	if _, payee := originalPayeeFailureCodes[value]; payee {
		return transferFailurePayee76
	}
	return transferFailureChannel76
}
func failureCodeLetter(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }

// Readable epay_release Transfer::$payee_err_code:47 entries,46 distinct.
// Preserve the provider's original spelling, including historical typos.
var originalPayeeFailureCodes = map[string]struct{}{
	"PAYEE_NOT_EXIST": {}, "PAYEE_ACCOUNT_STATUS_ERROR": {}, "CARD_BIN_ERROR": {}, "PAYEE_CARD_INFO_ERROR": {}, "PERM_AML_NOT_REALNAME_REV": {}, "PAYEE_USER_INFO_ERROR": {}, "PAYEE_ACC_OCUPIED": {}, "PERMIT_NON_BANK_LIMIT_PAYEE": {}, "PAYEE_TRUSTEESHIP_ACC_OVER_LIMIT": {}, "PAYEE_ACCOUNT_NOT_EXSIT": {}, "PAYEE_USERINFO_STATUS_ERROR": {}, "TRUSTEESHIP_RECIEVE_QUOTA_LIMIT": {}, "EXCEED_LIMIT_UNRN_DM_AMOUNT": {}, "INVALID_CARDNO": {}, "RELEASE_USER_FORBBIDEN_RECIEVE": {}, "PAYEE_USER_TYPE_ERROR": {}, "PAYEE_NOT_RELNAME_CERTIFY": {}, "PERMIT_LIMIT_PAYEE": {},
	"OPENID_ERROR": {}, "NAME_MISMATCH": {}, "V2_ACCOUNT_SIMPLE_BAN": {}, "MONEY_LIMIT": {}, "EXCEED_PAYEE_ACCOUNT_LIMIT": {}, "PAYEE_ACCOUNT_ABNORMAL": {}, "APPID_OR_OPENID_ERR": {},
	"REALNAME_CHECK_ERROR": {}, "RE_USER_NAME_CHECK_ERROR": {}, "ERR_TJ_BLACK": {}, "USER_FROZEN": {}, "TRANSFER_FAIL": {}, "TRANSFER_FEE_LIMIT_ERROR": {},
	"ACCOUNT_FROZEN": {}, "REAL_NAME_CHECK_FAIL": {}, "NAME_NOT_CORRECT": {}, "OPENID_INVALID": {}, "TRANSFER_QUOTA_EXCEED": {}, "DAY_RECEIVED_QUOTA_EXCEED": {}, "MONTH_RECEIVED_QUOTA_EXCEED": {}, "DAY_RECEIVED_COUNT_EXCEED": {}, "ID_CARD_NOT_CORRECT": {}, "ACCOUNT_NOT_EXIST": {}, "TRANSFER_RISK": {}, "REALNAME_ACCOUNT_RECEIVED_QUOTA_EXCEED": {}, "RECEIVE_ACCOUNT_NOT_PERMMIT": {}, "BLOCK_B2C_USERLIMITAMOUNT_BSRULE_MONTH": {}, "BLOCK_B2C_USERLIMITAMOUNT_MONTH": {},
}
