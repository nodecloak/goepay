package systemupdate

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

type Target struct {
	Fingerprint  string `json:"fingerprint"`
	Version      string `json:"version"`
	SourceSHA    string `json:"source_sha256"`
	PackageSHA   string `json:"package_sha256"`
	Schema       int64  `json:"schema"`
	PackageBytes int64  `json:"package_bytes"`
	FileCount    int    `json:"file_count"`
}
type Progress struct {
	Phase         string `json:"phase"`
	ReadBytes     int64  `json:"read_bytes"`
	VerifiedFiles int    `json:"verified_files"`
	TotalBytes    int64  `json:"total_bytes"`
	TotalFiles    int    `json:"total_files"`
}
type Ticket struct {
	service  *Service
	snapshot snapshot
	build    Build
}
type Artifact struct {
	root      *os.Root
	temporary string
	ticket    *Ticket
	receipt   []byte
	committed bool
}
type artifactReceipt struct {
	Fingerprint string            `json:"fingerprint"`
	Build       Build             `json:"build"`
	Applied     map[string]string `json:"applied"`
}

func (s *Service) Plan(ctx context.Context, b Build, fp string) (*Ticket, error) {
	snapshot, e := s.inspect(ctx, b)
	if e != nil {
		return nil, e
	}
	if !digestPattern.MatchString(fp) || snapshot.view.Fingerprint != fp || !snapshot.view.CanPrepare {
		return nil, ErrChanged
	}
	return &Ticket{service: s, snapshot: snapshot, build: normalizeBuild(b)}, nil
}
func (t *Ticket) Target() Target {
	m := t.snapshot.manifest
	return Target{t.snapshot.view.Fingerprint, m.Version, m.SourceSHA, m.Package.SHA256, m.TargetSchema, m.Package.Bytes, len(m.Package.Files)}
}
func (t *Ticket) AppliedCompatible(applied map[string]string) error {
	expected := t.DeclaredMigrations()
	var highest int64
	for _, f := range t.snapshot.manifest.Package.Files {
		if strings.HasPrefix(f.Name, "db/migrations/") {
			name := strings.TrimPrefix(f.Name, "db/migrations/")
			var n int64
			if _, e := fmt.Sscanf(name, "%d_", &n); e != nil {
				return ErrPackage
			}
			if n > highest {
				highest = n
			}
		}
	}
	for n, h := range applied {
		if !migrationPattern.MatchString(n) || !digestPattern.MatchString(h) || expected[n] != h {
			return ErrPackage
		}
	}
	if highest != t.snapshot.manifest.TargetSchema {
		return ErrPackage
	}
	return nil
}
func (t *Ticket) DeclaredMigrations() map[string]string {
	out := map[string]string{}
	for _, f := range t.snapshot.manifest.Package.Files {
		if strings.HasPrefix(f.Name, "db/migrations/") {
			out[strings.TrimPrefix(f.Name, "db/migrations/")] = f.SHA256
		}
	}
	return out
}

// VerifyPackage is also used by the offline verifier. It never publishes a
// staging artifact or executes package contents.
func (s *Service) VerifyPackage(ctx context.Context, b Build, fp string, applied map[string]string) error {
	t, e := s.Plan(ctx, b, fp)
	if e != nil {
		return e
	}
	a, e := t.Stage(ctx, applied, nil)
	if e != nil {
		return e
	}
	return a.Abort()
}
func stagingRoot(dir string, create bool) (*os.Root, error) {
	if create {
		if e := os.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
			return nil, ErrPackage
		}
	}
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrPackage
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, ErrPackage
	}
	return root, nil
}
func writeRoot(root *os.Root, name string, raw []byte) error {
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return ErrPackage
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	closeError := f.Close()
	if e != nil || closeError != nil {
		return ErrPackage
	}
	return nil
}
func copyMeasured(ctx context.Context, dst io.Writer, src io.Reader, maximum int64, report func(int64)) (int64, error) {
	buffer := make([]byte, 64*1024)
	var count int64
	for {
		if e := ctx.Err(); e != nil {
			return count, e
		}
		n, e := src.Read(buffer)
		if n > 0 {
			count += int64(n)
			if count > maximum {
				return count, ErrPackage
			}
			if _, err := dst.Write(buffer[:n]); err != nil {
				return count, ErrPackage
			}
			if report != nil {
				report(count)
			}
		}
		if e == io.EOF {
			return count, nil
		}
		if e != nil {
			return count, ErrPackage
		}
	}
}
func (t *Ticket) Stage(ctx context.Context, applied map[string]string, report func(Progress)) (artifact *Artifact, err error) {
	if t == nil || t.service == nil {
		return nil, ErrPackage
	}
	if e := t.AppliedCompatible(applied); e != nil {
		return nil, e
	}
	root, e := stagingRoot(t.service.config.StagingDir, true)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		root.Close()
		return nil, ErrPackage
	}
	a := &Artifact{root: root, temporary: ".preparing-" + hex.EncodeToString(nonce), ticket: t}
	if e = root.Mkdir(a.temporary, 0700); e != nil {
		root.Close()
		return nil, ErrPackage
	}
	defer func() {
		if err != nil {
			a.Abort()
		}
	}()
	m := t.snapshot.manifest
	p := Progress{Phase: "reading", TotalBytes: m.Package.Bytes, TotalFiles: len(m.Package.Files)}
	emit := func() {
		if report != nil {
			report(p)
		}
	}
	emit()
	info, e := os.Lstat(t.service.config.PackagePath)
	if e != nil || !info.Mode().IsRegular() || info.Size() != m.Package.Bytes {
		return nil, ErrPackage
	}
	original, e := os.Open(t.service.config.PackagePath)
	if e != nil {
		return nil, ErrPackage
	}
	defer original.Close()
	opened, e := original.Stat()
	if e != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, ErrPackage
	}
	output, e := root.OpenFile(a.temporary+"/bundle.zip", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, ErrPackage
	}
	hash := sha256.New()
	n, copyError := copyMeasured(ctx, io.MultiWriter(output, hash), original, m.Package.Bytes, func(n int64) { p.ReadBytes = n; emit() })
	syncError := output.Sync()
	closeError := output.Close()
	if copyError != nil {
		return nil, copyError
	}
	if syncError != nil || closeError != nil || n != m.Package.Bytes || hex.EncodeToString(hash.Sum(nil)) != m.Package.SHA256 {
		return nil, ErrPackage
	}
	p.Phase = "verifying"
	emit()
	bundle, e := root.Open(a.temporary + "/bundle.zip")
	if e != nil {
		return nil, ErrPackage
	}
	defer bundle.Close()
	if e = preflightDirectory(bundle, m.Package.Bytes); e != nil {
		return nil, e
	}
	reader, e := zip.NewReader(bundle, m.Package.Bytes)
	if e != nil {
		return nil, ErrPackage
	}
	if e = verifyZIP(ctx, reader, m, root, a.temporary+"/files", func(n int) { p.VerifiedFiles = n; emit() }); e != nil {
		return nil, e
	}
	checkpoint := ""
	for _, f := range m.Package.Files {
		if f.Name == "SOURCE_CHECKPOINT.json" {
			checkpoint = f.SHA256
		}
	}
	if checkpoint != m.SourceSHA {
		return nil, ErrPackage
	}
	api := "bin/api"
	if m.GOOS == "windows" {
		api += ".exe"
	}
	found := false
	for _, f := range m.Package.Files {
		if f.Name == api {
			found = true
		}
	}
	if !found {
		return nil, ErrPackage
	}
	a.receipt, e = json.Marshal(artifactReceipt{Fingerprint: t.Target().Fingerprint, Build: t.build, Applied: applied})
	if e != nil {
		return nil, ErrPackage
	}
	if writeRoot(root, a.temporary+"/manifest.json", t.snapshot.envelope) != nil || writeRoot(root, a.temporary+"/receipt.json", a.receipt) != nil {
		return nil, ErrPackage
	}
	p.Phase = "staged"
	emit()
	return a, nil
}

// Check the central-directory count and byte boundaries before archive/zip can
// allocate entries. ZIP64 and multipart archives cannot be needed at 64 MiB.
func preflightDirectory(f io.ReaderAt, size int64) error {
	if size < 22 || size > maxPackageBytes {
		return ErrPackage
	}
	tailSize := size
	if tailSize > 65557 {
		tailSize = 65557
	}
	tail := make([]byte, int(tailSize))
	if _, e := f.ReadAt(tail, size-tailSize); e != nil {
		return ErrPackage
	}
	end := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == 0x06054b50 && i+22+int(binary.LittleEndian.Uint16(tail[i+20:])) == len(tail) {
			end = i
			break
		}
	}
	if end < 0 {
		return ErrPackage
	}
	eocd := tail[end:]
	if binary.LittleEndian.Uint16(eocd[4:]) != 0 || binary.LittleEndian.Uint16(eocd[6:]) != 0 {
		return ErrPackage
	}
	count := int(binary.LittleEndian.Uint16(eocd[10:]))
	if count <= 0 || count > 2048 || binary.LittleEndian.Uint16(eocd[8:]) != uint16(count) {
		return ErrPackage
	}
	length, offset := int64(binary.LittleEndian.Uint32(eocd[12:])), int64(binary.LittleEndian.Uint32(eocd[16:]))
	if length <= 0 || offset < 0 || offset+length != size-tailSize+int64(end) {
		return ErrPackage
	}
	stop := offset + length
	records := 0
	head := make([]byte, 46)
	for offset < stop {
		if stop-offset < 46 {
			return ErrPackage
		}
		if _, e := f.ReadAt(head, offset); e != nil {
			return ErrPackage
		}
		if binary.LittleEndian.Uint32(head) != 0x02014b50 || binary.LittleEndian.Uint16(head[8:])&1 != 0 || binary.LittleEndian.Uint32(head[20:]) == 0xffffffff || binary.LittleEndian.Uint32(head[24:]) == 0xffffffff || binary.LittleEndian.Uint32(head[42:]) == 0xffffffff {
			return ErrPackage
		}
		name := int64(binary.LittleEndian.Uint16(head[28:]))
		length := int64(46) + name + int64(binary.LittleEndian.Uint16(head[30:])) + int64(binary.LittleEndian.Uint16(head[32:]))
		if name <= 0 || name > 241 || length > stop-offset {
			return ErrPackage
		}
		offset += length
		records++
		if records > 2048 {
			return ErrPackage
		}
	}
	if records != count || offset != stop {
		return ErrPackage
	}
	return nil
}
func verifyZIP(ctx context.Context, z *zip.Reader, m Manifest, root *os.Root, prefix string, progress func(int)) error {
	expected := map[string]File{}
	directories := map[string]bool{}
	for _, f := range m.Package.Files {
		expected[f.Name] = f
		for d := path.Dir(f.Name); d != "."; d = path.Dir(d) {
			directories[d] = true
		}
	}
	seen := map[string]bool{}
	verified := 0
	for _, f := range z.File {
		if e := ctx.Err(); e != nil {
			return e
		}
		name := f.Name
		isDir := f.FileInfo().IsDir()
		if isDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" || seen[strings.ToLower(name)] || f.Mode()&os.ModeSymlink != 0 || f.Mode()&os.ModeType != 0 && !isDir || (f.Method != zip.Store && f.Method != zip.Deflate) {
			return ErrPackage
		}
		seen[strings.ToLower(name)] = true
		if isDir {
			if !directories[name] || f.UncompressedSize64 != 0 {
				return ErrPackage
			}
			continue
		}
		spec, ok := expected[name]
		if !ok || !allowedName(name) || uint64(spec.Bytes) != f.UncompressedSize64 {
			return ErrPackage
		}
		input, e := f.Open()
		if e != nil {
			return ErrPackage
		}
		hash := sha256.New()
		var output *os.File
		var writer io.Writer = hash
		if root != nil {
			if e = root.MkdirAll(prefix+"/"+path.Dir(name), 0700); e == nil {
				output, e = root.OpenFile(prefix+"/"+name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			}
			if e != nil {
				input.Close()
				return ErrPackage
			}
			writer = io.MultiWriter(output, hash)
		}
		n, copyError := copyMeasured(ctx, writer, input, spec.Bytes, nil)
		closeError := input.Close()
		if output != nil {
			if e = output.Sync(); e != nil {
				copyError = ErrPackage
			}
			if e = output.Close(); e != nil {
				copyError = ErrPackage
			}
		}
		if copyError != nil {
			return copyError
		}
		if closeError != nil || n != spec.Bytes || hex.EncodeToString(hash.Sum(nil)) != spec.SHA256 {
			return ErrPackage
		}
		delete(expected, name)
		verified++
		if progress != nil {
			progress(verified)
		}
	}
	if len(expected) != 0 {
		return ErrPackage
	}
	return nil
}
func (a *Artifact) Seal() string { h := sha256.Sum256(a.receipt); return hex.EncodeToString(h[:]) }
func (a *Artifact) Abort() error {
	if a == nil || a.root == nil {
		return nil
	}
	var e error
	if !a.committed {
		e = a.root.RemoveAll(a.temporary)
	}
	closeError := a.root.Close()
	a.root = nil
	if e != nil || closeError != nil {
		return ErrPackage
	}
	return nil
}
func (a *Artifact) Commit(ctx context.Context, repair ...bool) (string, error) {
	if a == nil || a.root == nil || a.committed {
		return "", ErrPackage
	}
	if len(repair) > 1 {
		return "", ErrPackage
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	seal := a.Seal()
	intent := a.ticket.Target().Fingerprint
	if _, e := a.root.Lstat(intent); e == nil {
		target, e := a.ticket.service.VerifiedArtifact(ctx, intent, seal)
		if e == nil && target == a.ticket.Target() {
			if e = a.root.RemoveAll(a.temporary); e != nil {
				return "", ErrPackage
			}
		} else {
			if len(repair) != 1 || !repair[0] {
				return "", ErrChanged
			}
			// A fresh, explicit state-CAS repair may quarantine an unusable
			// artifact. Never follow or overwrite its entries, or a runtime path.
			nonce := make([]byte, 16)
			if _, e = rand.Read(nonce); e != nil {
				return "", ErrPackage
			}
			if e = a.root.Rename(intent, ".quarantine-"+hex.EncodeToString(nonce)); e != nil {
				return "", ErrPackage
			}
			if e = a.root.Rename(a.temporary, intent); e != nil {
				return "", ErrPackage
			}
		}
	} else if !os.IsNotExist(e) {
		return "", ErrPackage
	} else if e = a.root.Rename(a.temporary, intent); e != nil {
		return "", ErrPackage
	}
	a.committed = true
	a.root.Close()
	a.root = nil
	return seal, nil
}
func (s *Service) VerifiedArtifact(ctx context.Context, intent, seal string) (Target, error) {
	var empty Target
	if s == nil || !digestPattern.MatchString(intent) || !digestPattern.MatchString(seal) {
		return empty, ErrPackage
	}
	root, e := stagingRoot(s.config.StagingDir, false)
	if e != nil {
		return empty, e
	}
	defer root.Close()
	directory, e := root.Lstat(intent)
	if e != nil || !directory.IsDir() || directory.Mode()&os.ModeSymlink != 0 {
		return empty, ErrPackage
	}
	read := func(name string, max int64) ([]byte, error) {
		info, e := root.Lstat(intent + "/" + name)
		if e != nil || !info.Mode().IsRegular() || info.Size() > max {
			return nil, ErrPackage
		}
		f, e := root.Open(intent + "/" + name)
		if e != nil {
			return nil, ErrPackage
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(&contextReader{ctx, f}, max+1))
	}
	raw, e := read("receipt.json", maxManifestBytes)
	if e != nil {
		return empty, e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != seal {
		return empty, ErrPackage
	}
	var receipt artifactReceipt
	if strictJSON(raw, &receipt) != nil || receipt.Fingerprint != intent {
		return empty, ErrPackage
	}
	envelope, e := read("manifest.json", maxManifestBytes)
	if e != nil {
		return empty, e
	}
	var signed struct {
		Manifest  json.RawMessage `json:"manifest"`
		Signature string          `json:"signature"`
	}
	if strictJSON(envelope, &signed) != nil {
		return empty, ErrPackage
	}
	signature, e := base64.RawURLEncoding.DecodeString(signed.Signature)
	if e != nil || !ed25519.Verify(s.config.PublicKey, append([]byte(manifestDomain), signed.Manifest...), signature) {
		return empty, ErrPackage
	}
	var m Manifest
	if strictJSON(signed.Manifest, &m) != nil || validateManifest(m) != nil {
		return empty, ErrPackage
	}
	allowed := map[string]bool{intent + "/receipt.json": true, intent + "/manifest.json": true, intent + "/bundle.zip": true}
	dirs := map[string]bool{intent: true, intent + "/files": true}
	checkpoint := ""
	api := "bin/api"
	if m.GOOS == "windows" {
		api += ".exe"
	}
	apiFound := false
	for _, f := range m.Package.Files {
		allowed[intent+"/files/"+f.Name] = true
		for d := path.Dir(intent + "/files/" + f.Name); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
		if f.Name == "SOURCE_CHECKPOINT.json" {
			checkpoint = f.SHA256
		}
		if f.Name == api {
			apiFound = true
		}
	}
	if checkpoint != m.SourceSHA || !apiFound {
		return empty, ErrPackage
	}
	if e = fs.WalkDir(root.FS(), intent, func(n string, d fs.DirEntry, e error) error {
		if e != nil || ctx.Err() != nil {
			return ErrPackage
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrPackage
		}
		if d.IsDir() {
			if !dirs[n] {
				return ErrPackage
			}
			return nil
		}
		if !allowed[n] || !d.Type().IsRegular() {
			return ErrPackage
		}
		return nil
	}); e != nil {
		return empty, ErrPackage
	}
	buildRaw, _ := json.Marshal(receipt.Build)
	fp := sha256.New()
	fp.Write([]byte("goepay/update183/state\x00"))
	fp.Write(buildRaw)
	fp.Write([]byte{0})
	fp.Write(s.config.PublicKey)
	fp.Write(envelope)
	if hex.EncodeToString(fp.Sum(nil)) != intent {
		return empty, ErrPackage
	}
	t := &Ticket{service: s, build: receipt.Build, snapshot: snapshot{manifest: m, envelope: envelope, view: View{Fingerprint: intent}}}
	if e = t.AppliedCompatible(receipt.Applied); e != nil {
		return empty, e
	}
	f, e := root.Open(intent + "/bundle.zip")
	if e != nil {
		return empty, ErrPackage
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != m.Package.Bytes {
		return empty, ErrPackage
	}
	hash := sha256.New()
	if n, e := copyMeasured(ctx, hash, f, m.Package.Bytes, nil); e != nil || n != m.Package.Bytes || hex.EncodeToString(hash.Sum(nil)) != m.Package.SHA256 {
		return empty, ErrPackage
	}
	if preflightDirectory(f, m.Package.Bytes) != nil {
		return empty, ErrPackage
	}
	z, e := zip.NewReader(f, m.Package.Bytes)
	if e != nil || verifyZIP(ctx, z, m, nil, "", nil) != nil {
		return empty, ErrPackage
	}
	for _, spec := range m.Package.Files {
		file, e := root.Lstat(intent + "/files/" + spec.Name)
		if e != nil || !file.Mode().IsRegular() || file.Size() != spec.Bytes {
			return empty, ErrPackage
		}
		f, e := root.Open(intent + "/files/" + spec.Name)
		if e != nil {
			return empty, ErrPackage
		}
		hash := sha256.New()
		n, e := copyMeasured(ctx, hash, f, spec.Bytes, nil)
		f.Close()
		if e != nil || n != spec.Bytes || hex.EncodeToString(hash.Sum(nil)) != spec.SHA256 {
			return empty, ErrPackage
		}
	}
	return t.Target(), nil
}
