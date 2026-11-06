package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"goepay/internal/channelconfig"
	"goepay/internal/compat"
)

// strictChannelImport has the import's fields without its UnmarshalJSON, so the
// plaintext can be decoded with unknown-field rejection: a misspelled key such
// as "app_type" must fail here instead of silently dropping a setting.
type strictChannelImport channelconfig.Import

func masterKeyFromEnvironment() ([]byte, error) {
	key, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if e != nil || len(key) != 32 {
		return nil, fmt.Errorf("GOEPAY_MASTER_KEY must be 64 hex characters")
	}
	return key, nil
}

// sealChannels reads a plaintext channel import, validates it the way API
// startup does (known plugins, unique IDs, buildable providers) and writes the
// sealed GOEPAY_CHANNEL_CONFIG_FILE envelope to out. The report names channel
// IDs and plugins only; configuration values and certificates are never echoed.
func sealChannels(in io.Reader, out, report io.Writer, master []byte) error {
	raw, e := io.ReadAll(io.LimitReader(in, 4<<20+1))
	if e != nil {
		return fmt.Errorf("read plaintext channel import: %w", e)
	}
	if len(raw) > 4<<20 {
		return fmt.Errorf("plaintext channel import exceeds 4 MiB")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	var plain strictChannelImport
	if e = d.Decode(&plain); e != nil {
		return fmt.Errorf("invalid plaintext channel import JSON: %w", e)
	}
	if _, e = d.Token(); e != io.EOF {
		return fmt.Errorf("invalid plaintext channel import JSON: trailing data")
	}
	imp := channelconfig.Import(plain)
	if imp.Version != 1 {
		return fmt.Errorf(`plaintext channel import must declare "version": 1`)
	}
	if len(imp.Channels) == 0 {
		return fmt.Errorf("at least one channel is required; console channels are registered under these source channels")
	}
	for _, c := range imp.Channels {
		if _, e = channelconfig.Build(channelconfig.Import{Version: 1, Channels: []channelconfig.Channel{c}}, false); e != nil {
			return fmt.Errorf("channel %d (%s): configuration is incomplete or invalid for this plugin", c.ID, c.Plugin)
		}
	}
	if _, e = channelconfig.Build(imp, false); e != nil {
		return fmt.Errorf("channel import is invalid as a whole (check for duplicate channel IDs)")
	}
	sealed, e := channelconfig.Encrypt(imp, master)
	if e != nil {
		return fmt.Errorf("channel import could not be sealed: %w", e)
	}
	if _, e = out.Write(append(sealed, '\n')); e != nil {
		return e
	}
	for _, c := range imp.Channels {
		fmt.Fprintf(report, "sealed channel %d plugin=%s app_types=%s\n", c.ID, c.Plugin, strings.Join(c.AppTypes, ","))
	}
	fmt.Fprintln(report, "Keep the plaintext out of the server; the sealed file opens only with this GOEPAY_MASTER_KEY.")
	return nil
}

// generatePlatformKey creates the platform RSA signing key that V2 responses
// and V2 merchant notifications are signed with (GOEPAY_PLATFORM_PRIVATE_KEY_FILE
// for the API and worker). It never overwrites an existing file and writes the
// public key, which merchants use to verify signatures, to out.
func generatePlatformKey(path string, out io.Writer) (err error) {
	if path == "" {
		return fmt.Errorf("set GOEPAY_PLATFORM_PRIVATE_KEY_FILE to the path of the new private key")
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return e
	}
	der, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if e != nil {
		if errors.Is(e, os.ErrExist) {
			return fmt.Errorf("%s already exists; a platform key is never overwritten (merchants verify with its public key)", path)
		}
		return fmt.Errorf("create platform key file: %w", e)
	}
	defer func() {
		if err != nil {
			os.Remove(path)
		}
	}()
	if e = pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	written, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if _, e = compat.ParsePrivateKey(string(written)); e != nil {
		return fmt.Errorf("generated platform key could not be read back: %w", e)
	}
	public, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		return e
	}
	return pem.Encode(out, &pem.Block{Type: "PUBLIC KEY", Bytes: public})
}
