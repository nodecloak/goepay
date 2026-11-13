package cryptochain

import "encoding/binary"

// RLP is serialization only. Hashing uses x/crypto and all signing belongs to
// the isolated signer, which independently reconstructs this encoding.
func encodeRLPList(items [][]byte) []byte {
	var body []byte
	for _, item := range items {
		if len(item) == 1 && item[0] < 0x80 {
			body = append(body, item[0])
			continue
		}
		body = append(body, rlpPrefix(len(item), 0x80, 0xb7)...)
		body = append(body, item...)
	}
	return append(rlpPrefix(len(body), 0xc0, 0xf7), body...)
}
func rlpPrefix(size int, short, long byte) []byte {
	if size < 56 {
		return []byte{short + byte(size)}
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(size))
	i := 0
	for i < 7 && b[i] == 0 {
		i++
	}
	return append([]byte{long + byte(8-i)}, b[i:]...)
}

func decodeRLPList(raw []byte) ([][]byte, error) {
	if len(raw) == 0 || len(raw) > 16384 || raw[0] < 0xc0 {
		return nil, ErrInvalidResponse
	}
	body, tail, list, err := rlpItem(raw)
	if err != nil || !list || len(tail) != 0 {
		return nil, ErrInvalidResponse
	}
	var out [][]byte
	for len(body) > 0 {
		item, rest, isList, err := rlpItem(body)
		if err != nil || isList {
			return nil, ErrInvalidResponse
		}
		out = append(out, item)
		body = rest
		if len(out) > 9 {
			return nil, ErrInvalidResponse
		}
	}
	return out, nil
}
func rlpItem(raw []byte) (item, rest []byte, list bool, err error) {
	if len(raw) == 0 {
		return nil, nil, false, ErrInvalidResponse
	}
	prefix := raw[0]
	if prefix < 0x80 {
		return raw[:1], raw[1:], false, nil
	}
	offset := 1
	size := 0
	switch {
	case prefix <= 0xb7:
		size = int(prefix - 0x80)
	case prefix <= 0xbf:
		width := int(prefix - 0xb7)
		if len(raw) < 1+width || raw[1] == 0 {
			return nil, nil, false, ErrInvalidResponse
		}
		offset += width
		for _, b := range raw[1:offset] {
			if size > 16384 {
				return nil, nil, false, ErrInvalidResponse
			}
			size = size*256 + int(b)
		}
		if size < 56 {
			return nil, nil, false, ErrInvalidResponse
		}
	case prefix <= 0xf7:
		list = true
		size = int(prefix - 0xc0)
	default:
		list = true
		width := int(prefix - 0xf7)
		if len(raw) < 1+width || raw[1] == 0 {
			return nil, nil, false, ErrInvalidResponse
		}
		offset += width
		for _, b := range raw[1:offset] {
			if size > 16384 {
				return nil, nil, false, ErrInvalidResponse
			}
			size = size*256 + int(b)
		}
		if size < 56 {
			return nil, nil, false, ErrInvalidResponse
		}
	}
	if size > len(raw)-offset {
		return nil, nil, false, ErrInvalidResponse
	}
	if !list && size == 1 && raw[offset] < 0x80 {
		return nil, nil, false, ErrInvalidResponse
	}
	return raw[offset : offset+size], raw[offset+size:], list, nil
}
