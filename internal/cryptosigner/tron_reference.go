package cryptosigner

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"goepay/internal/cryptochain"
)

type tronBlock struct {
	BlockID     string `json:"blockID"`
	BlockHeader struct {
		RawData struct {
			Number uint64 `json:"number"`
		} `json:"raw_data"`
	} `json:"block_header"`
}

func verifyTRONReference(ctx context.Context, r Rule, refBytes, refHash []byte, refNum uint64) error {
	u, err := url.Parse(r.TRONRPCURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return fmt.Errorf("invalid trusted TRON RPC URL")
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(path string, params any) (tronBlock, error) {
		var result tronBlock
		b, _ := json.Marshal(params)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.TRONRPCURL, "/")+path, bytes.NewReader(b))
		if err != nil {
			return result, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return result, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return result, fmt.Errorf("trusted TRON RPC returned %d", resp.StatusCode)
		}
		b, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return result, err
		}
		err = json.Unmarshal(b, &result)
		return result, err
	}
	genesis, err := call("/wallet/getblockbynum", map[string]uint64{"num": 0})
	if err != nil {
		return err
	}
	if genesis.BlockID != cryptochain.TronMainnetGenesis {
		return fmt.Errorf("trusted TRON RPC is on wrong network")
	}
	head, err := call("/wallet/getnowblock", map[string]any{})
	if err != nil {
		return err
	}
	height := head.BlockHeader.RawData.Number
	headID, err := hex.DecodeString(head.BlockID)
	if err != nil || len(headID) != 32 || binary.BigEndian.Uint64(headID[:8]) != height {
		return fmt.Errorf("invalid trusted TRON head")
	}
	low := uint64(binary.BigEndian.Uint16(refBytes))
	candidate := height&^uint64(65535) | low
	if candidate > height {
		if candidate < 65536 {
			return fmt.Errorf("invalid TRON reference height")
		}
		candidate -= 65536
	}
	if refNum != 0 && refNum != candidate {
		return fmt.Errorf("TRON reference height mismatch")
	}
	block, err := call("/wallet/getblockbynum", map[string]uint64{"num": candidate})
	if err != nil {
		return err
	}
	id, err := hex.DecodeString(block.BlockID)
	if err != nil || len(id) != 32 || binary.BigEndian.Uint64(id[:8]) != candidate || block.BlockHeader.RawData.Number != candidate || !bytes.Equal(id[8:16], refHash) {
		return fmt.Errorf("TRON reference does not belong to configured network")
	}
	return nil
}
