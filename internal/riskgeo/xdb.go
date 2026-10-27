// Package riskgeo reads a locally supplied ip2region xdb. It performs no
// network requests and ships no geographic database or proprietary source.
package riskgeo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Region struct{ Country, Province, City string }
type Resolver interface {
	Lookup(netip.Addr) (Region, bool, error)
}
type XDB struct{ IPv4Path, IPv6Path string }

func localPath(p string) bool {
	return filepath.IsAbs(p) && !strings.HasPrefix(p, `\\`) && !strings.HasPrefix(p, "//") && !strings.Contains(p, "://")
}
func (x XDB) Ready() bool {
	for _, p := range []string{x.IPv4Path, x.IPv6Path} {
		if !localPath(p) {
			return false
		}
		f, e := os.Open(p)
		if e != nil {
			return false
		}
		st, e := f.Stat()
		f.Close()
		if e != nil || !st.Mode().IsRegular() || st.Size() < 256+65536*8 {
			return false
		}
	}
	return true
}

var ErrDatabase = errors.New("local geographic database unavailable or invalid")

func (x XDB) Lookup(ip netip.Addr) (Region, bool, error) {
	if !ip.IsValid() {
		return Region{}, false, ErrDatabase
	}
	ip = ip.Unmap()
	path := x.IPv6Path
	n := 16
	if ip.Is4() {
		path = x.IPv4Path
		n = 4
	}
	if !localPath(path) {
		return Region{}, false, ErrDatabase
	}
	f, e := os.Open(path)
	if e != nil {
		return Region{}, false, ErrDatabase
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return Region{}, false, ErrDatabase
	}
	return lookup(f, st.Size(), ip, n)
}
func lookup(r io.ReaderAt, size int64, ip netip.Addr, n int) (Region, bool, error) {
	raw := ip.AsSlice()
	if len(raw) != n || size < 256+65536*8 {
		return Region{}, false, ErrDatabase
	}
	read := func(off int64, b []byte) error {
		if off < 0 || int64(len(b)) > size-off {
			return ErrDatabase
		}
		_, e := r.ReadAt(b, off)
		if e != nil {
			return ErrDatabase
		}
		return nil
	}
	vector := make([]byte, 8)
	if e := read(256+int64(int(raw[0])*256+int(raw[1]))*8, vector); e != nil {
		return Region{}, false, e
	}
	start, end := int64(binary.LittleEndian.Uint32(vector)), int64(binary.LittleEndian.Uint32(vector[4:]))
	if start == 0 && end == 0 {
		return Region{}, false, nil
	}
	stride := int64(n*2 + 6)
	if start < 256+65536*8 || end < start || (end-start)%stride != 0 || end+stride > size {
		return Region{}, false, ErrDatabase
	}
	compare := func(segment []byte) int {
		if n == 4 {
			var rev [4]byte
			for i := 0; i < 4; i++ {
				rev[i] = segment[3-i]
			}
			return bytes.Compare(raw, rev[:])
		}
		return bytes.Compare(raw, segment)
	}
	lo, hi := int64(0), (end-start)/stride
	for lo <= hi {
		mid := lo + (hi-lo)/2
		b := make([]byte, stride)
		if e := read(start+mid*stride, b); e != nil {
			return Region{}, false, e
		}
		if compare(b[:n]) < 0 {
			hi = mid - 1
			continue
		}
		if compare(b[n:n*2]) > 0 {
			lo = mid + 1
			continue
		}
		length := int(binary.LittleEndian.Uint16(b[n*2:]))
		ptr := int64(binary.LittleEndian.Uint32(b[n*2+2:]))
		if length == 0 {
			return Region{}, false, nil
		}
		if length > 8192 {
			return Region{}, false, ErrDatabase
		}
		data := make([]byte, length)
		if e := read(ptr, data); e != nil {
			return Region{}, false, e
		}
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return Region{}, false, ErrDatabase
		}
		parts := strings.Split(string(data), "|")
		if len(parts) < 3 {
			return Region{}, false, ErrDatabase
		}
		clean := func(s string) string {
			if s == "0" {
				return ""
			}
			return strings.TrimSpace(s)
		}
		return Region{Country: clean(parts[0]), Province: clean(parts[1]), City: clean(parts[2])}, true, nil
	}
	return Region{}, false, nil
}
