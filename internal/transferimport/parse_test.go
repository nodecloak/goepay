package transferimport

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

const header = "序号,收款账号,收款人姓名,转账金额（元）,转账备注\r\n"

func TestCSVPreservesPrivateTextAndExactMoney(t *testing.T) {
	d, e := Parse("csv", []byte("\xef\xbb\xbf"+header+"1,000123456789012345678,张三,0.10,\"first,second\"\r\n2,test@example.invalid,李四,1.2,\r\n"))
	if e != nil || len(d.Rows) != 2 || d.TotalCents != 130 || d.Rows[0].Account != "000123456789012345678" || d.Rows[1].Amount != "1.20" || d.Rows[0].Remark != "first,second" {
		t.Fatalf("valid draft lost precision or text: count=%d total=%d err=%v", len(d.Rows), d.TotalCents, e)
	}
}

func TestCSVRejectsAmbiguousOrUnsafeRowsWithoutEcho(t *testing.T) {
	cases := []string{header + "1,private-account,Private Name,1.001,\n", header + "1,private-account,Private Name,1e3,\n", header + "1,private-account,Private Name,0,\n", header + "1,private-account,Private Name,-1,\n", header + "1,private-account,Private Name,99999999999999999999,\n", header + "1,private-account,Private Name,1,\n1,other,Other,1,\n", header + "1,private-account,Private Name,,\n", header + "1,=HYPERLINK(1),Private Name,1,\n", header + "1,private-account,Private Name,1,extra,six\n", "wrong,header\n1,private-account,Private Name,1,\n", header + "1,private-account,Private Name,NaN,\n", header + "1,private-account,Private Name,1,\x00\n"}
	for _, raw := range cases {
		d, e := Parse("csv", []byte(raw))
		if e == nil || len(d.Rows) != 0 {
			t.Fatal("invalid file yielded partial draft")
		}
		if strings.Contains(e.Error(), "private-account") || strings.Contains(e.Error(), "Private Name") {
			t.Fatal("private row leaked in error")
		}
	}
	var b strings.Builder
	b.WriteString(header)
	for i := 1; i <= 101; i++ {
		fmt.Fprintf(&b, "%d,account,,1,\n", i)
	}
	if _, e := Parse("csv", []byte(b.String())); e == nil {
		t.Fatal("101 rows accepted")
	}
	if _, e := Parse("xls", []byte("legacy")); e == nil {
		t.Fatal("unsupported XLS silently accepted")
	}
	if _, e := Parse("csv", bytes.Repeat([]byte("a"), MaxFileBytes+1)); e == nil {
		t.Fatal("oversized file accepted")
	}
}

func workbook(t *testing.T, sheet string, extras map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	parts := map[string]string{"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`, "xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`, "xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + sheet + `</sheetData></worksheet>`}
	for k, v := range extras {
		parts[k] = v
	}
	for k, v := range parts {
		w, e := z.Create(k)
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(v))
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func cells(row int, values []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<row r="%d">`, row)
	for i, v := range values {
		fmt.Fprintf(&b, `<c r="%c%d" t="inlineStr"><is><t>%s</t></is></c>`, 'A'+i, row, v)
	}
	b.WriteString(`</row>`)
	return b.String()
}
func xheader() string {
	return cells(1, []string{"序号", "收款账号", "收款人姓名", "转账金额（元）", "转账备注"})
}

func TestXLSXOriginalTemplateAndInlineStrings(t *testing.T) {
	raw, e := os.ReadFile("testdata/original-transfer.xlsx")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Parse("xlsx", raw); e == nil || !strings.Contains(e.Error(), "empty") {
		t.Fatalf("original empty template not recognized: %v", e)
	}
	d, e := Parse("xlsx", workbook(t, xheader()+cells(2, []string{"1", "000123456789012345678", "张三", "0.01", "safe"}), nil))
	if e != nil || len(d.Rows) != 1 || d.TotalCents != 1 || d.Rows[0].Account != "000123456789012345678" {
		t.Fatalf("xlsx text draft rejected count=%d total=%d err=%v", len(d.Rows), d.TotalCents, e)
	}
}
func TestXLSXRejectsFormulaExternalCoordinatePrecisionAndBudget(t *testing.T) {
	valid := xheader() + cells(2, []string{"1", "private-account", "Private Name", "1", ""})
	cases := [][]byte{
		workbook(t, strings.Replace(valid, `<is><t>1</t></is>`, `<f>1+1</f><v>2</v>`, 1), nil),
		workbook(t, valid, map[string]string{"xl/_rels/sheet1.xml.rels": `<Relationships><Relationship TargetMode="External" Target="https://secret.invalid"/></Relationships>`}),
		workbook(t, strings.Replace(valid, `r="B2"`, `r="A2"`, 1), nil),
		workbook(t, strings.Replace(valid, `r="E2"`, `r="F2"`, 1), nil),
		workbook(t, strings.Replace(valid, `<c r="B2" t="inlineStr"><is><t>private-account</t></is></c>`, `<c r="B2"><v>123456789012345678</v></c>`, 1), nil),
		workbook(t, valid, map[string]string{"xl/huge.xml": "<x>" + strings.Repeat("A", 5*1024*1024) + "</x>"}),
		workbook(t, valid, map[string]string{"../outside.xml": "<x/>"}),
		workbook(t, valid, map[string]string{"xl/dtd.xml": `<!DOCTYPE x [<!ENTITY foo "bar">]><x>&foo;</x>`}),
		workbook(t, valid, map[string]string{"xl/vbaProject.bin": "macro"}),
		workbook(t, valid, map[string]string{"docProps/core.xml": "<one/><two/>"}),
		workbook(t, strings.Replace(valid, `r="B2" t="inlineStr"`, `r="B2" r="B2" t="inlineStr"`, 1), nil),
		workbook(t, valid, map[string]string{"xl/_rels/other.xml.rels": `<Relationships><Relationship Id="unused" Target="https://external.invalid/data"/></Relationships>`}),
		workbook(t, valid, map[string]string{"[Content_Types].xml": `<broken/>`}),
	}
	for i, raw := range cases {
		d, e := Parse("xlsx", raw)
		if e == nil || len(d.Rows) != 0 {
			t.Fatalf("unsafe workbook %d accepted", i)
		}
		if strings.Contains(e.Error(), "private-account") || strings.Contains(e.Error(), "Private Name") {
			t.Fatal("private content leaked")
		}
	}
}

func TestXLSXSharedStringsEntryAndRowBudgets(t *testing.T) {
	valid := xheader() + cells(2, []string{"1", "account", "Name", "1", ""})
	var shared strings.Builder
	shared.WriteString(`<sst>`)
	for i := 0; i < 1025; i++ {
		shared.WriteString(`<si><t>bounded</t></si>`)
	}
	shared.WriteString(`</sst>`)
	if _, e := Parse("xlsx", workbook(t, valid, map[string]string{"xl/sharedStrings.xml": shared.String()})); e == nil {
		t.Fatal("shared string limit ignored")
	}
	extras := map[string]string{}
	for i := 0; i < 65; i++ {
		extras[fmt.Sprintf("docProps/p%d.xml", i)] = `<p/>`
	}
	if _, e := Parse("xlsx", workbook(t, valid, extras)); e == nil {
		t.Fatal("ZIP entry limit ignored")
	}
	var rows strings.Builder
	rows.WriteString(xheader())
	for i := 2; i <= 102; i++ {
		rows.WriteString(cells(i, []string{fmt.Sprint(i - 1), "account", "Name", "1", ""}))
	}
	if _, e := Parse("xlsx", workbook(t, rows.String(), nil)); e == nil {
		t.Fatal("worksheet physical row limit ignored")
	}
	var csv strings.Builder
	csv.WriteString(header)
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&csv, "%d,account,,0.01,\n", i)
	}
	d, e := Parse("csv", []byte(csv.String()))
	if e != nil || len(d.Rows) != 100 || d.TotalCents != 100 {
		t.Fatal("valid 100-row boundary rejected")
	}
}
