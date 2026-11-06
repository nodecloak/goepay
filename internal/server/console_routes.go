package server

import (
	"net/http"

	"goepay/internal/identity"
)

// consoleHandler serves console requests and reports whether it handled the
// request. token is the session cookie value.
type consoleHandler func(s *Server, w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool

// consoleRoute names a handler and the route families (the path segment after
// /console/v1/<role>/) it serves. A request is offered only to the routes of its
// own family, in table order, so ordering matters only between handlers that
// share a family. TestConsoleRoutesMatchHandlerFamilies keeps every list equal
// to the families its handler actually compares.
type consoleRoute struct {
	name     string
	families []string
	serve    consoleHandler
}

func withToken(f func(*Server, http.ResponseWriter, *http.Request, string, identity.Session, []string, string) bool) consoleHandler {
	return f
}

func withoutToken(f func(*Server, http.ResponseWriter, *http.Request, string, identity.Session, []string) bool) consoleHandler {
	return func(s *Server, w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, _ string) bool {
		return f(s, w, r, role, session, parts)
	}
}

// consoleRoutesBeforeFunds run before funds-action protection is attached.
var consoleRoutesBeforeFunds = []consoleRoute{
	{"migrationWizardConsole234", []string{"migration"}, withToken((*Server).migrationWizardConsole234)},
	{"transferRoutingConsole228", []string{"settings"}, withToken((*Server).transferRoutingConsole228)},
	{"captchaSettingsConsole203", []string{"settings"}, withToken((*Server).captchaSettingsConsole203)},
	{"proxyCredentialsConsole", []string{"settings"}, withToken((*Server).proxyCredentialsConsole)},
	{"voiceSettings230", []string{"settings"}, withToken((*Server).voiceSettings230)},
	{"certification233Console", []string{"account", "settings"}, withToken((*Server).certification233Console)},
	{"noticeAccounts231", []string{"settings"}, withToken((*Server).noticeAccounts231)},
	{"noticeTemplates230", []string{"settings"}, withToken((*Server).noticeTemplates230)},
	{"oauthCredentialsConsole", []string{"settings"}, withToken((*Server).oauthCredentialsConsole)},
	{"adminUserSecurityConsole", []string{"admin-users"}, withoutToken((*Server).adminUserSecurityConsole)},
	{"proxyConnectivityConsole", []string{"settings"}, withToken((*Server).proxyConnectivityConsole)},
	{"orderAPITradeAliasConsole120", []string{"orders"}, func(s *Server, w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
		return s.orderAPITradeAliasConsole120(w, r, role, session, parts, token, s.OrderAPITradeAlias120)
	}},
	{"paymentDefendSettingsConsole206", []string{"settings"}, func(s *Server, w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
		return s.paymentDefendSettingsConsole206(w, r, role, session, parts, token, s.PaymentDefend206)
	}},
	{"ssoConsole", []string{"sso"}, withToken((*Server).ssoConsole)},
	{"fundsSecurity", []string{"security"}, withToken((*Server).fundsSecurity)},
}

// consoleRoutesAfterFunds receive the request carrying funds-action protection.
var consoleRoutesAfterFunds = []consoleRoute{
	{"applicationAuthorizationConsole", []string{"application-authorizations"}, withToken((*Server).applicationAuthorizationConsole)},
	{"weixinApplicationConsole", []string{"weixin-applications"}, withToken((*Server).weixinApplicationConsole)},
	{"IdentityProfileConsole", []string{"identity-profile-lookups"}, withToken((*Server).IdentityProfileConsole)},
	{"IdentityLookupConsole", []string{"identity-lookups"}, withToken((*Server).IdentityLookupConsole)},
	{"channelSharingConsole", []string{"channel-sharing"}, withoutToken((*Server).channelSharingConsole)},
	{"orderIncomeConsole", []string{"orders"}, withoutToken((*Server).orderIncomeConsole)},
	{"refundFundingConsole", []string{"orders"}, withoutToken((*Server).refundFundingConsole)},
	{"refundAttestationConsole", []string{"refunds"}, withToken((*Server).refundAttestationConsole)},
	{"merchantDashboardTips", []string{"dashboard"}, withoutToken((*Server).merchantDashboardTips)},
	{"legacyRefundHistoryConsole", []string{"legacy-refund-history"}, withoutToken((*Server).legacyRefundHistoryConsole)},
	{"combinedOrdersConsole204", []string{"orders"}, withoutToken((*Server).combinedOrdersConsole204)},
	{"preauthorizationObservationConsole", []string{"orders"}, withToken((*Server).preauthorizationObservationConsole)},
	{"preauthorizationCaptureConsole", []string{"orders"}, withoutToken((*Server).preauthorizationCaptureConsole)},
	{"notificationResendConsole", []string{"orders"}, withoutToken((*Server).notificationResendConsole)},
	{"notificationReturnConsole200", []string{"orders"}, withToken((*Server).notificationReturnConsole200)},
	{"preauthorizationRemainingConsole", []string{"orders"}, withToken((*Server).preauthorizationRemainingConsole)},
	{"authorizationReleaseConsole", []string{"orders"}, withoutToken((*Server).authorizationReleaseConsole)},
	{"orderBlacklistConsole", []string{"orders"}, withoutToken((*Server).orderBlacklistConsole)},
	{"orderBusinessBatchConsole", []string{"orders"}, withToken((*Server).orderBusinessBatchConsole)},
	{"orderBatchControlsConsole", []string{"orders"}, withoutToken((*Server).orderBatchControlsConsole)},
	{"orderAdminActions", []string{"orders"}, withoutToken((*Server).orderAdminActions)},
	{"membershipSalesConsole", []string{"membership-sales"}, withoutToken((*Server).membershipSalesConsole)},
	{"invitationCodesConsole", []string{"invitation-codes"}, withoutToken((*Server).invitationCodesConsole)},
	{"collectionStylesConsole", []string{"collection-styles"}, withoutToken((*Server).collectionStylesConsole)},
	{"collectionGeneratorConsole", []string{"collection-generator"}, withoutToken((*Server).collectionGeneratorConsole)},
	{"collectionStyleImagesConsole", []string{"collection-style-images"}, withoutToken((*Server).collectionStyleImagesConsole)},
	// Before orderOperations: its GET /orders/<trade_no> would read "summary"
	// as an order number and answer "order not found".
	{"orderSummary", []string{"orders"}, withoutToken((*Server).orderSummary)},
	{"orderOperations", []string{"orders"}, withoutToken((*Server).orderOperations)},
	{"transferConsole", []string{"transfers"}, withoutToken((*Server).transferConsole)},
	{"redpackConsole", []string{"redpacks"}, withoutToken((*Server).redpackConsole)},
	{"transferBatchConsole", []string{"transfer-batches"}, withoutToken((*Server).transferBatchConsole)},
	{"transferBatchImport", []string{"transfer-batch-imports"}, withoutToken((*Server).transferBatchImport)},
	{"referralConsole", []string{"referral", "referral-policies"}, withoutToken((*Server).referralConsole)},
	{"membershipReferralConsole", []string{"membership-referral-policies", "workflows"}, withoutToken((*Server).membershipReferralConsole)},
	{"channelStopConsole", []string{"channel-stops"}, withToken((*Server).channelStopConsole)},
	{"settlementAutoConsole", []string{"settlement-plans"}, withoutToken((*Server).settlementAutoConsole)},
	{"settlementPlanConsole", []string{"account", "settlement-plan-authorizations", "settlement-plan-runs", "settlement-plans"}, withoutToken((*Server).settlementPlanConsole)},
	{"printingConsole", []string{"account", "orders", "print-jobs", "printing"}, withoutToken((*Server).printingConsole)},
	{"merchantNoticeConsole", []string{"account", "merchants"}, withoutToken((*Server).merchantNoticeConsole)},
	{"adminMerchantSignaturePolicy", []string{"merchants"}, withToken((*Server).adminMerchantSignaturePolicy)},
	{"systemUpdateConsole", []string{"system-update"}, withToken((*Server).systemUpdateConsole)},
	{"mailFrequency194", []string{"settings"}, withToken((*Server).mailFrequency194)},
	{"mailAccounts195", []string{"settings"}, withToken((*Server).mailAccounts195)},
	{"smsAccounts134", []string{"settings"}, withToken((*Server).smsAccounts134)},
	{"businessSMSSettings231", []string{"settings"}, withToken((*Server).businessSMSSettings231)},
	{"siteBrandConsole193", []string{"settings"}, withToken((*Server).siteBrandConsole193)},
	{"scanJSSettingsConsole202", []string{"settings"}, withToken((*Server).scanJSSettingsConsole202)},
	{"merchantScanPlatform192", []string{"settings"}, withToken((*Server).merchantScanPlatform192)},
	{"platformMerchantAccessSettings", []string{"settings"}, withToken((*Server).platformMerchantAccessSettings)},
	{"buyerMinimumConsole104", []string{"buyer-fees"}, withToken((*Server).buyerMinimumConsole104)},
	{"runtimeChannelLimits", []string{"channel-policies"}, withToken((*Server).runtimeChannelLimits)},
	{"paymentSubchannelConsole", []string{"payment-subchannels"}, withToken((*Server).paymentSubchannelConsole)},
	{"paymentParentChannels", []string{"payment-channels"}, withToken((*Server).paymentParentChannels)},
	{"runtimeChannelPolicies", []string{"channel-policies"}, withToken((*Server).runtimeChannelPolicies)},
	{"scheduledTasks", []string{"scheduled-tasks"}, withToken((*Server).scheduledTasks)},
	{"adminMerchantCredentials", []string{"merchants"}, withToken((*Server).adminMerchantCredentials)},
	{"corePaymentRisk", []string{"payment-risk"}, withToken((*Server).corePaymentRisk)},
	{"businessNoticeConsole", []string{"business-notices"}, withoutToken((*Server).businessNoticeConsole)},
	{"templateConsole", []string{"templates"}, withoutToken((*Server).templateConsole)},
	{"settlementPayoutConsole", []string{"settlement-payouts"}, withoutToken((*Server).settlementPayoutConsole)},
	{"settlementBatchConsole", []string{"settlement-batches"}, withoutToken((*Server).settlementBatchConsole)},
	{"collectionLink", []string{"collection-codes"}, withoutToken((*Server).collectionLink)},
	{"security", []string{"security"}, withoutToken((*Server).security)},
	{"operations", []string{"operations"}, withoutToken((*Server).operations)},
	{"merchantScanJS202", []string{"scanpay"}, withToken((*Server).merchantScanJS202)},
	{"merchantScanConsole192", []string{"scanpay"}, withToken((*Server).merchantScanConsole192)},
	{"accountBindings", []string{"account"}, withoutToken((*Server).accountBindings)},
	{"account", []string{"account"}, withoutToken((*Server).account)},
	{"adminMerchantLoginLog", []string{"merchants"}, withoutToken((*Server).adminMerchantLoginLog)},
	{"refundSummary", []string{"refunds"}, withoutToken((*Server).refundSummary)},
	{"payTypeOptions", []string{"pay-type-options"}, withoutToken((*Server).payTypeOptions)},
	{"ledgerSummary", []string{"ledger"}, withoutToken((*Server).ledgerSummary)},
	{"reports", []string{"reports"}, withoutToken((*Server).reports)},
	{"complaintsConsole", []string{"complaint-channels", "complaint-sync-tasks", "complaints", "complaints-charts", "complaints-image", "complaints-summary", "complaints-sync", "complaints133"}, withoutToken((*Server).complaintsConsole)},
	{"backoffice", []string{"manage", "resources", "workflows"}, withoutToken((*Server).backoffice)},
}

// serveConsoleRoutes offers the request to the routes of its family in order.
func (s *Server) serveConsoleRoutes(routes []consoleRoute, w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	for _, route := range routes {
		for _, family := range route.families {
			if family == parts[1] {
				if route.serve(s, w, r, role, session, parts, token) {
					return true
				}
				break
			}
		}
	}
	return false
}
