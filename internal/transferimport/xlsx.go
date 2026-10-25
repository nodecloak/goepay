package transferimport

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"strconv"
	"strings"
)

const sheetNamespace = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"

// loadPackage bounds actual and declared expansion before constructing XML trees.
// Every XML part is scanned, including unused sheets, so hidden formula/external
// link parts cannot be smuggled alongside the first worksheet.
func loadPackage(raw []byte) (map[string][]byte, error) {
	z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil || len(z.File) > 64 {
		return nil, invalid(0, "invalid_xlsx_archive")
	}
	files := map[string][]byte{}
	total := 0
	for _, f := range z.File {
		n := f.Name
		lower := strings.ToLower(n)
		if n == "" || path.Clean(n) != n || strings.Contains(n, "\\") || strings.HasPrefix(n, "/") || strings.HasPrefix(n, "../") || strings.Contains(n, ":") || files[n] != nil || f.FileInfo().IsDir() {
			return nil, invalid(0, "invalid_archive_path")
		}
		if strings.Contains(lower, "externallinks") || strings.Contains(lower, "vba") || strings.Contains(lower, "embedding") || (!strings.HasSuffix(lower, ".xml") && !strings.HasSuffix(lower, ".rels")) {
			return nil, invalid(0, "active_or_external_content_not_allowed")
		}
		if f.UncompressedSize64 > 1<<20 || uint64(total)+f.UncompressedSize64 > 4<<20 {
			return nil, invalid(0, "xlsx_expansion_limit")
		}
		r, e := f.Open()
		if e != nil {
			return nil, invalid(0, "invalid_xlsx_entry")
		}
		b, e := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		closeErr := r.Close()
		if e != nil || closeErr != nil || len(b) > 1<<20 || uint64(len(b)) != f.UncompressedSize64 {
			return nil, invalid(0, "invalid_xlsx_entry")
		}
		total += len(b)
		if total > 4<<20 {
			return nil, invalid(0, "xlsx_expansion_limit")
		}
		if e = scanXML(b); e != nil {
			return nil, e
		}
		files[n] = b
	}
	if files["[Content_Types].xml"] == nil || files["xl/workbook.xml"] == nil || files["xl/_rels/workbook.xml.rels"] == nil {
		return nil, invalid(0, "xlsx_package_parts_missing")
	}
	var contentTypes struct {
		XMLName   xml.Name
		Overrides []struct {
			Part string `xml:"PartName,attr"`
			Type string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if xml.Unmarshal(files["[Content_Types].xml"], &contentTypes) != nil || contentTypes.XMLName.Local != "Types" || contentTypes.XMLName.Space != "http://schemas.openxmlformats.org/package/2006/content-types" {
		return nil, invalid(0, "invalid_content_types")
	}
	mainCount := 0
	for _, v := range contentTypes.Overrides {
		if v.Part == "/xl/workbook.xml" {
			if v.Type != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml" {
				return nil, invalid(0, "unsupported_workbook_content_type")
			}
			mainCount++
		}
	}
	if mainCount != 1 {
		return nil, invalid(0, "workbook_content_type_required")
	}
	return files, nil
}
func scanXML(b []byte) error {
	d := xml.NewDecoder(bytes.NewReader(b))
	depth, tokens, roots := 0, 0, 0
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return invalid(0, "invalid_xml")
		}
		tokens++
		if tokens > 30000 {
			return invalid(0, "xml_token_limit")
		}
		switch v := t.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return invalid(0, "multiple_xml_roots")
				}
			}
			depth++
			if depth > 24 {
				return invalid(0, "xml_depth_limit")
			}
			if v.Name.Local == "f" || v.Name.Local == "externalLink" || v.Name.Local == "ddeLink" || v.Name.Local == "oleObject" {
				return invalid(0, "formula_or_external_content_not_allowed")
			}
			attrs := map[xml.Name]bool{}
			for _, a := range v.Attr {
				if attrs[a.Name] {
					return invalid(0, "duplicate_xml_attribute")
				}
				attrs[a.Name] = true
				if a.Name.Local == "Target" && (strings.Contains(a.Value, ":") || strings.HasPrefix(a.Value, "//")) {
					return invalid(0, "external_target_not_allowed")
				}
				if a.Name.Local == "TargetMode" && a.Value != "Internal" || strings.Contains(strings.ToLower(a.Value), "macroenabled") {
					return invalid(0, "external_or_macro_content_not_allowed")
				}
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return invalid(0, "xml_directive_not_allowed")
		case xml.ProcInst:
			if v.Target != "xml" {
				return invalid(0, "xml_instruction_not_allowed")
			}
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(v)) > 0 {
				return invalid(0, "xml_text_outside_root")
			}
			if len(v) > 16384 {
				return invalid(0, "xml_text_limit")
			}
		}
	}
	if depth != 0 {
		return invalid(0, "invalid_xml")
	}
	return nil
}

type sharedText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (s sharedText) value() (string, error) {
	v := s.Text
	for _, r := range s.Runs {
		v += r.Text
	}
	if len(v) > 4096 {
		return "", invalid(0, "cell_text_limit")
	}
	return v, nil
}
func parseXLSX(raw []byte) ([][]string, []int, error) {
	files, e := loadPackage(raw)
	if e != nil {
		return nil, nil, e
	}
	var book struct {
		XMLName xml.Name
		Sheets  []struct {
			ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(files["xl/workbook.xml"], &book) != nil || book.XMLName.Local != "workbook" || book.XMLName.Space != sheetNamespace || len(book.Sheets) == 0 || book.Sheets[0].ID == "" {
		return nil, nil, invalid(0, "invalid_workbook")
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Type   string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(files["xl/_rels/workbook.xml.rels"], &rels) != nil {
		return nil, nil, invalid(0, "invalid_workbook_relationships")
	}
	target := ""
	seenRel := map[string]bool{}
	for _, r := range rels.Items {
		if r.ID == "" || seenRel[r.ID] {
			return nil, nil, invalid(0, "ambiguous_relationship")
		}
		seenRel[r.ID] = true
		if r.ID == book.Sheets[0].ID {
			if !strings.HasSuffix(r.Type, "/worksheet") || strings.Contains(r.Target, "..") || strings.ContainsAny(r.Target, "\\:") {
				return nil, nil, invalid(0, "invalid_sheet_target")
			}
			target = path.Join("xl", r.Target)
			if strings.HasPrefix(r.Target, "/xl/") {
				target = strings.TrimPrefix(r.Target, "/")
			}
		}
	}
	if target == "" || files[target] == nil || !strings.HasPrefix(target, "xl/worksheets/") {
		return nil, nil, invalid(0, "worksheet_missing")
	}
	var shared []string
	if b := files["xl/sharedStrings.xml"]; b != nil {
		var ss struct {
			XMLName xml.Name
			Items   []sharedText `xml:"si"`
		}
		if xml.Unmarshal(b, &ss) != nil || ss.XMLName.Local != "sst" || len(ss.Items) > 1024 {
			return nil, nil, invalid(0, "shared_string_limit_or_format")
		}
		for _, v := range ss.Items {
			x, e := v.value()
			if e != nil {
				return nil, nil, e
			}
			shared = append(shared, x)
		}
	}
	var sheet struct {
		XMLName xml.Name
		Rows    []struct {
			Index int `xml:"r,attr"`
			Cells []struct {
				Ref    string     `xml:"r,attr"`
				Type   string     `xml:"t,attr"`
				Value  string     `xml:"v"`
				Inline sharedText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if xml.Unmarshal(files[target], &sheet) != nil || sheet.XMLName.Local != "worksheet" || sheet.XMLName.Space != sheetNamespace || len(sheet.Rows) > 101 {
		return nil, nil, invalid(0, "worksheet_format_or_row_limit")
	}
	var table [][]string
	var positions []int
	last := 0
	for _, r := range sheet.Rows {
		if r.Index <= last || r.Index > 101 || len(r.Cells) > 5 {
			return nil, nil, invalid(0, "invalid_row_coordinates")
		}
		last = r.Index
		row := make([]string, 5)
		seen := map[int]bool{}
		for _, c := range r.Cells {
			if len(c.Ref) < 2 || c.Ref[0] < 'A' || c.Ref[0] > 'E' || c.Ref[1:] != strconv.Itoa(r.Index) {
				return nil, nil, invalid(r.Index, "invalid_cell_coordinates")
			}
			col := int(c.Ref[0] - 'A')
			if seen[col] {
				return nil, nil, invalid(r.Index, "duplicate_cell_coordinates")
			}
			seen[col] = true
			v := c.Value
			switch c.Type {
			case "s":
				i, e := strconv.Atoi(v)
				if e != nil || i < 0 || i >= len(shared) {
					return nil, nil, invalid(r.Index, "invalid_shared_string")
				}
				v = shared[i]
			case "inlineStr":
				v, e = c.Inline.value()
				if e != nil {
					return nil, nil, e
				}
			case "", "n":
				if col == 1 && v != "" {
					return nil, nil, invalid(r.Index, "account_must_be_text")
				}
			default:
				return nil, nil, invalid(r.Index, "unsupported_cell_type")
			}
			row[col] = v
		}
		table = append(table, row)
		positions = append(positions, r.Index)
	}
	return table, positions, nil
}
