package payment

import (
	"bytes"
	"encoding/csv"
	"goepay/internal/ledger"
	"golang.org/x/text/encoding/simplifiedchinese"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type SettlementBatchExport struct {
	Filename, ContentType string
	Data                  []byte `json:"-"`
}

func (SettlementBatchExport) String() string   { return "payment.SettlementBatchExport(redacted)" }
func (SettlementBatchExport) GoString() string { return "payment.SettlementBatchExport(redacted)" }

type settlementBatchExportRow struct {
	RequestID                  int64
	Position                   int
	Type, Account, Name, AppID string
	NetAmount                  int64
}

func safeSettlementCSVCell(v string) bool {
	if !utf8.ValidString(v) || len(v) > 4096 {
		return false
	}
	for _, c := range v {
		if c < 32 || c == 127 || unicode.Is(unicode.Cf, c) {
			return false
		}
	}
	trim := strings.TrimSpace(v)
	return trim == "" || !strings.ContainsRune("=+-@", rune(trim[0]))
}
func renderSettlementBatchExport(batch SettlementBatch, rows []settlementBatchExportRow, format, remark string) (SettlementBatchExport, error) {
	var empty SettlementBatchExport
	if format == "summary" {
		format = "common"
	}
	if format != "common" && format != "alipay" && format != "mybank" && format != "wxpay" {
		return empty, ErrInvalid
	}
	if len(batch.Batch) < 20 || len(batch.Batch) > 27 || len(rows) < 1 || len(rows) > 100 || len(rows) != batch.Count || !safeSettlementCSVCell(remark) {
		return empty, ErrInvalid
	}
	for _, c := range batch.Batch {
		if c < '0' || c > '9' {
			return empty, ErrInvalid
		}
	}
	selected := []settlementBatchExportRow{}
	seen := map[int64]bool{}
	ranks := map[string]int{"alipay": 1, "wxpay": 2, "bank": 4}
	for _, r := range rows {
		if ranks[r.Type] == 0 || r.RequestID <= 0 || seen[r.RequestID] || r.Position <= 0 || r.NetAmount <= 0 || r.Account == "" || r.Type != "wxpay" && r.Name == "" || !safeSettlementCSVCell(r.Account) || !safeSettlementCSVCell(r.Name) || !safeSettlementCSVCell(r.AppID) {
			return empty, ErrInvalid
		}
		seen[r.RequestID] = true
		if format == "common" || format == "alipay" && r.Type == "alipay" || format == "mybank" && (r.Type == "alipay" || r.Type == "bank") || format == "wxpay" && r.Type == "wxpay" {
			selected = append(selected, r)
		}
	}
	if len(selected) == 0 {
		return empty, ErrState
	}
	sort.Slice(selected, func(i, j int) bool {
		if format == "common" && selected[i].Type != selected[j].Type {
			return ranks[selected[i].Type] < ranks[selected[j].Type]
		}
		return selected[i].RequestID < selected[j].RequestID
	})
	records := [][]string{}
	switch format {
	case "mybank":
		records = append(records, []string{"收款方名称", "收款方账号", "收款方开户行名称", "收款行联行号", "金额", "附言/用途"})
	case "alipay":
		records = append(records, []string{"支付宝批量付款文件模板"}, []string{"序号（必填）", "收款方支付宝账号（必填）", "收款方姓名（必填）", "金额（必填，单位：元）", "备注（选填）"})
	case "common":
		records = append(records, []string{"序号", "转账方式", "收款账号", "收款人姓名", "转账金额（元）", "转账备注"})
	case "wxpay":
		var total int64
		app := ""
		for _, r := range selected {
			if r.AppID == "" {
				return empty, ErrState
			}
			if app != "" && app != r.AppID {
				return empty, ErrConflict
			}
			app = r.AppID
			if total > math.MaxInt64-r.NetAmount {
				return empty, ErrInvalid
			}
			total += r.NetAmount
		}
		records = append(records, []string{"微信支付批量转账到零钱模版（勿删）"}, []string{"商家批次单号（必填）", batch.Batch}, []string{"批次名称（必填）", "批量转账" + batch.Batch}, []string{"转账appid（必填）", app}, []string{"转账总金额（必填，单位：元）", strings.TrimRight(strings.TrimRight(ledger.Format(total), "0"), ".")}, []string{"转账总笔数（必填）", strconv.Itoa(len(selected))}, []string{"批次备注（必填）", "批量转账" + batch.Batch}, []string{"", ""}, []string{"转账明细（勿删）"}, []string{"商家明细单号（必填）", "收款用户openid（必填）", "收款用户姓名（选填）", "收款用户身份证（选填）", "转账金额（必填，单位：元）", "转账备注（必填）"})
	}
	for i, r := range selected {
		money := ledger.Format(r.NetAmount)
		switch format {
		case "alipay":
			records = append(records, []string{strconv.Itoa(i + 1), r.Account, r.Name, money, remark})
		case "mybank":
			bank := ""
			if r.Type == "alipay" {
				bank = "支付宝"
			}
			records = append(records, []string{r.Name, r.Account, bank, "", money, remark})
		case "wxpay":
			records = append(records, []string{batch.Batch + strconv.Itoa(i+1), r.Account, r.Name, "", money, remark})
		case "common":
			name := map[string]string{"alipay": "支付宝", "wxpay": "微信", "bank": "银行卡"}[r.Type]
			records = append(records, []string{strconv.Itoa(i + 1), name, r.Account, r.Name, money, remark})
		}
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	writer.UseCRLF = true
	writer.WriteAll(records)
	if err := writer.Error(); err != nil {
		return empty, err
	}
	raw := buf.Bytes()
	charset := "utf-8"
	if format == "alipay" || format == "mybank" {
		var err error
		raw, err = simplifiedchinese.GBK.NewEncoder().Bytes(raw)
		if err != nil {
			return empty, ErrInvalid
		}
		charset = "gbk"
	} else {
		raw = append([]byte{239, 187, 191}, raw...)
	}
	if len(raw) > 1024*1024 {
		return empty, ErrInvalid
	}
	return SettlementBatchExport{Filename: "pay_" + format + "_" + batch.Batch + ".csv", ContentType: "text/csv; charset=" + charset, Data: raw}, nil
}
