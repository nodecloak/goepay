package pagetemplate

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// An uploaded template package (上传模板) is a zip file with template.json,
// index.html and the files the page uses. ParsePackage reads it into memory
// within fixed limits and refuses anything it does not understand, so the
// rest of the code only sees checked paths and contents.

const (
	MaxPackageZip      = 10 << 20
	maxPackageEntries  = 2000
	maxPackageFiles    = 300
	maxPackageFile     = 3 << 20
	maxPackageTotal    = 24 << 20
	maxPackageIndex    = 512 << 10
	maxPackageManifest = 4 << 10
)

// UploadError is a refused package or upload operation, with a reason for the
// administrator.
type UploadError struct{ Reason string }

func (e *UploadError) Error() string { return e.Reason }
func (e *UploadError) Unwrap() error { return ErrInvalid }

func refuse(format string, args ...any) error {
	return &UploadError{fmt.Sprintf(format, args...)}
}

// Manifest is template.json.
type Manifest struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
}

// Package is a checked template package.
type Package struct {
	Manifest Manifest
	Files    map[string][]byte
	Ignored  []string
}

var (
	uploadNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)
	packageSegment    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._@+-]{0,79}$`)
	packageVersion    = regexp.MustCompile(`^[0-9A-Za-z.+-]{0,32}$`)
	uploadKeyPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reservedNames     = map[string]bool{"custom": true, "upload": true, "uploads": true, "sample": true, "selection": true, "images": true, "files": true, "preview": true, "versions": true}
	kindLabels        = map[string]string{"home": "首页模板", "pay": "支付页模板", "qrpay": "聚合收款页模板"}
)

// ValidUploadName reports whether name may name an uploaded template: the
// pattern, and never a built-in name of any kind or a console route word.
func ValidUploadName(name string) bool {
	if !uploadNamePattern.MatchString(name) || reservedNames[name] {
		return false
	}
	if strings.HasPrefix(name, "index") && strings.Trim(name[5:], "0123456789") == "" {
		return false
	}
	for _, kind := range []string{"home", "pay", "qrpay"} {
		if _, ok := Lookup(kind, name); ok {
			return false
		}
	}
	return true
}

type packageType struct {
	mime     string
	homeOnly bool
	check    func([]byte) bool
}

func prefixed(prefixes ...string) func([]byte) bool {
	return func(b []byte) bool {
		for _, p := range prefixes {
			if bytes.HasPrefix(b, []byte(p)) {
				return true
			}
		}
		return false
	}
}

func textFile(b []byte) bool {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	return utf8.Valid(b) && bytes.IndexByte(b, 0) < 0
}

var packageTypes = map[string]packageType{
	"css":   {"text/css; charset=utf-8", false, textFile},
	"png":   {"image/png", false, prefixed("\x89PNG\r\n\x1a\n")},
	"jpg":   {"image/jpeg", false, prefixed("\xff\xd8\xff")},
	"jpeg":  {"image/jpeg", false, prefixed("\xff\xd8\xff")},
	"gif":   {"image/gif", false, prefixed("GIF87a", "GIF89a")},
	"webp":  {"image/webp", false, func(b []byte) bool { return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" }},
	"avif":  {"image/avif", false, func(b []byte) bool { return len(b) >= 12 && string(b[4:8]) == "ftyp" }},
	"svg":   {"image/svg+xml", false, func(b []byte) bool { return textFile(b) && bytes.Contains(bytes.ToLower(b), []byte("<svg")) }},
	"ico":   {"image/x-icon", false, prefixed("\x00\x00\x01\x00", "\x89PNG\r\n\x1a\n")},
	"woff":  {"font/woff", false, prefixed("wOFF")},
	"woff2": {"font/woff2", false, prefixed("wOF2")},
	"ttf":   {"font/ttf", false, prefixed("\x00\x01\x00\x00", "true", "OTTO", "ttcf", "typ1")},
	"otf":   {"font/otf", false, prefixed("\x00\x01\x00\x00", "true", "OTTO", "ttcf", "typ1")},
	"js":    {"text/javascript; charset=utf-8", true, textFile},
	"mjs":   {"text/javascript; charset=utf-8", true, textFile},
	"json":  {"application/json", true, textFile},
	"mp4":   {"video/mp4", true, func(b []byte) bool { return len(b) >= 12 && string(b[4:8]) == "ftyp" }},
	"webm":  {"video/webm", true, prefixed("\x1a\x45\xdf\xa3")},
}

func extension(p string) string { return strings.TrimPrefix(strings.ToLower(path.Ext(p)), ".") }

// FileMIME is the content type a package file is served with; ok is false for
// files that are never served (the page source and the manifest).
func FileMIME(p string) (string, bool) {
	if p == "index.html" || p == "template.json" {
		return "", false
	}
	t, ok := packageTypes[extension(p)]
	return t.mime, ok
}

func isImage(p string) bool {
	switch extension(p) {
	case "png", "jpg", "jpeg", "gif", "webp", "avif", "svg", "ico":
		return true
	}
	return false
}

// ignoredEntry is what zip tools and editors add next to the real files.
func ignoredEntry(name string) bool {
	segments := strings.Split(name, "/")
	if segments[0] == "__MACOSX" {
		return true
	}
	for _, s := range segments {
		// Hidden files are leftovers; "." and ".." are refused as paths.
		if strings.HasPrefix(s, ".") && s != "." && s != ".." {
			return true
		}
	}
	base := segments[len(segments)-1]
	switch strings.ToLower(base) {
	case "thumbs.db", "desktop.ini", "license", "licence", "readme":
		return true
	}
	switch extension(base) {
	case "md", "txt", "map":
		return true
	}
	return false
}

// shown makes an entry name safe and short for an error message.
func shown(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == utf8.RuneError {
			return '?'
		}
		return r
	}, name)
	return clip(name, 80)
}

// ParsePackage reads and checks a template package for kind.
func ParsePackage(kind string, raw []byte) (*Package, error) {
	if !validKind(kind) {
		return nil, ErrInvalid
	}
	if len(raw) > MaxPackageZip {
		return nil, refuse("模板包超过 10 MiB")
	}
	zr, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		return nil, refuse("请上传 zip 格式的模板包")
	}
	if len(zr.File) > maxPackageEntries {
		return nil, refuse("压缩包里的条目太多")
	}
	type entry struct {
		name string
		file *zip.File
	}
	var kept []entry
	var ignored []string
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if strings.HasSuffix(name, "/") || f.FileInfo().IsDir() {
			continue
		}
		if f.Flags&0x1 != 0 {
			return nil, refuse("压缩包不能加密：%s", shown(name))
		}
		if !f.Mode().IsRegular() {
			return nil, refuse("压缩包里只能有普通文件：%s", shown(name))
		}
		if ignoredEntry(strings.TrimPrefix(name, "/")) {
			ignored = append(ignored, shown(name))
			continue
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return nil, refuse("不支持的压缩方式：%s", shown(name))
		}
		kept = append(kept, entry{name, f})
	}
	// A package zipped as one folder: strip that folder.
	if len(kept) > 0 {
		root := false
		for _, k := range kept {
			if k.name == "template.json" {
				root = true
			}
		}
		if prefix, _, found := strings.Cut(kept[0].name, "/"); !root && found && prefix != "" {
			same := true
			for _, k := range kept {
				if !strings.HasPrefix(k.name, prefix+"/") {
					same = false
					break
				}
			}
			if same {
				for i := range kept {
					kept[i].name = strings.TrimPrefix(kept[i].name, prefix+"/")
				}
			}
		}
	}
	if len(kept) > maxPackageFiles {
		return nil, refuse("模板包最多 %d 个文件", maxPackageFiles)
	}
	folded := map[string]string{}
	pkg := &Package{Files: map[string][]byte{}, Ignored: ignored}
	var total int64
	scriptFile := "" // a home-only file in a pay or qrpay package
	for _, k := range kept {
		segments := strings.Split(k.name, "/")
		valid := len(k.name) <= 200 && len(segments) <= 8
		for _, s := range segments {
			if !packageSegment.MatchString(s) {
				valid = false
			}
		}
		if !valid {
			return nil, refuse("文件名只能用英文字母、数字和 . _ - @ +，最多 8 层：%s", shown(k.name))
		}
		lower := strings.ToLower(k.name)
		if other, dup := folded[lower]; dup {
			return nil, refuse("文件名重复或只差大小写：%s 和 %s", shown(other), shown(k.name))
		}
		folded[lower] = k.name
		ext := extension(k.name)
		limit := int64(maxPackageFile)
		switch {
		case k.name == "index.html":
			limit = maxPackageIndex
		case k.name == "template.json":
			limit = maxPackageManifest
		case ext == "html" || ext == "htm":
			return nil, refuse("模板包只能有一个页面 index.html，放在最外层：%s", shown(k.name))
		default:
			t, ok := packageTypes[ext]
			if !ok {
				return nil, refuse("不支持的文件类型：%s", shown(k.name))
			}
			if t.homeOnly && kind != "home" && scriptFile == "" {
				scriptFile = k.name
			}
		}
		if k.file.UncompressedSize64 > uint64(limit) {
			return nil, refuse("文件 %s 超过 %s", shown(k.name), sizeText(limit))
		}
		rc, e := k.file.Open()
		if e != nil {
			return nil, refuse("压缩包已损坏：%s", shown(k.name))
		}
		content, e := io.ReadAll(io.LimitReader(rc, limit+1))
		rc.Close()
		if e != nil {
			return nil, refuse("压缩包已损坏：%s", shown(k.name))
		}
		if int64(len(content)) > limit {
			return nil, refuse("文件 %s 超过 %s", shown(k.name), sizeText(limit))
		}
		total += int64(len(content))
		if total > maxPackageTotal {
			return nil, refuse("模板包解压后超过 24 MiB")
		}
		pkg.Files[k.name] = content
	}
	manifest, ok := pkg.Files["template.json"]
	if !ok {
		return nil, refuse("模板包里缺少 template.json（放在最外层）")
	}
	m, e := parseManifest(manifest)
	if e != nil {
		return nil, e
	}
	if m.Type != kind {
		return nil, refuse("这是%s，请到「%s」里上传", kindLabels[m.Type], kindLabels[m.Type])
	}
	if scriptFile != "" {
		return nil, refuse("支付页和收款页模板不运行脚本，也不能带 %s 文件：%s", extension(scriptFile), shown(scriptFile))
	}
	if e := checkFiles(kind, pkg.Files); e != nil {
		return nil, e
	}
	pkg.Manifest = m
	return pkg, nil
}

func sizeText(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d KiB", n>>10)
}

// checkFiles checks the stored or uploaded files of a package of kind: the
// required files, every type and every content. Restored versions are
// checked again with the rules of today.
func checkFiles(kind string, files map[string][]byte) error {
	if _, ok := files["template.json"]; !ok {
		return refuse("模板包里缺少 template.json（放在最外层）")
	}
	index, ok := files["index.html"]
	if !ok {
		return refuse("模板包里缺少 index.html（放在最外层）")
	}
	if len(index) > maxPackageIndex {
		return refuse("index.html 超过 512 KiB")
	}
	if !textFile(index) {
		return refuse("index.html 不是 UTF-8 文本")
	}
	if len(files) > maxPackageFiles {
		return refuse("模板包最多 %d 个文件", maxPackageFiles)
	}
	for p, content := range files {
		if p == "index.html" || p == "template.json" {
			continue
		}
		t, ok := packageTypes[extension(p)]
		if !ok || t.homeOnly && kind != "home" {
			return refuse("不支持的文件类型：%s", shown(p))
		}
		if !t.check(content) {
			return refuse("文件内容和类型不符：%s", shown(p))
		}
	}
	return nil
}

func plainText(s string, max int, required bool) bool {
	if required && strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 || r == 0x2028 || r == 0x2029 {
			return false
		}
	}
	return true
}

func parseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if len(raw) > maxPackageManifest || !utf8.Valid(raw) {
		return m, refuse("template.json 不是 UTF-8 文本或超过 4 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if t, e := d.Token(); e != nil || t != json.Delim('{') {
		return m, refuse("template.json 必须是一个 JSON 对象")
	}
	seen := map[string]bool{}
	fields := map[string]*string{"type": &m.Type, "name": &m.Name, "title": &m.Title, "version": &m.Version, "author": &m.Author, "description": &m.Description}
	for d.More() {
		t, e := d.Token()
		key, ok := t.(string)
		if e != nil || !ok {
			return m, refuse("template.json 格式不对")
		}
		target, known := fields[key]
		if !known {
			return m, refuse("template.json 有不认识的字段：%s", clip(shown(key), 32))
		}
		if seen[key] {
			return m, refuse("template.json 的字段 %s 重复了", key)
		}
		seen[key] = true
		if e = d.Decode(target); e != nil {
			return m, refuse("template.json 的字段 %s 必须是文字", key)
		}
	}
	if t, e := d.Token(); e != nil || t != json.Delim('}') {
		return m, refuse("template.json 格式不对")
	}
	if _, e := d.Token(); e != io.EOF {
		return m, refuse("template.json 格式不对")
	}
	switch {
	case !validKind(m.Type):
		return m, refuse("template.json 的 type 必须是 home、pay 或 qrpay")
	case !uploadNamePattern.MatchString(m.Name):
		return m, refuse("模板名称（name）只能用小写字母、数字和横线，2–32 个字符，以字母开头")
	case !ValidUploadName(m.Name):
		return m, refuse("模板名称 %s 是系统保留的名称，请换一个", m.Name)
	case !plainText(m.Title, 40, true):
		return m, refuse("模板标题（title）必须是 1–40 个字，不能有换行")
	case !packageVersion.MatchString(m.Version):
		return m, refuse("version 只能用字母、数字和 . + -，最多 32 个字符")
	case !plainText(m.Author, 40, false):
		return m, refuse("author 最多 40 个字，不能有换行")
	case !plainText(m.Description, 200, false):
		return m, refuse("description 最多 200 个字，不能有换行")
	}
	return m, nil
}

func digestHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// packageDigest identifies the files of a version: every path with the
// digest of its content.
func packageDigest(digests map[string]string) string {
	paths := make([]string, 0, len(digests))
	for p := range digests {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p + "\x00" + digests[p] + "\n")
	}
	return digestHex([]byte(b.String()))
}

// zipFiles writes files as a zip with stable order and times, so the same
// files always give the same archive.
func zipFiles(files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range paths {
		w, e := zw.CreateHeader(&zip.FileHeader{Name: p, Method: zip.Deflate, Modified: when})
		if e != nil {
			return nil, e
		}
		if _, e = w.Write(files[p]); e != nil {
			return nil, e
		}
	}
	if e := zw.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
