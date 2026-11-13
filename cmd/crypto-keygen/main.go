// crypto-keygen runs on the isolated signer host. Its stdout contains public
// addresses and a key handle only; secret key bytes go to one exclusive file.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"goepay/internal/cryptosigner"
)

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("crypto-keygen", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "", "existing signer-only private directory")
	id := flags.String("key-id", "", "new key handle (letters, digits, hyphens and underscores)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("invalid crypto-keygen arguments; use --dir and --key-id")
	}
	if *dir == "" || *id == "" || flags.NArg() != 0 {
		return fmt.Errorf("crypto-keygen requires --dir and --key-id")
	}
	public, err := cryptosigner.ProvisionKey(*dir, *id)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(out).Encode(public); err != nil {
		return fmt.Errorf("public address output failed; preserve and inspect the already-created local key file: %w", err)
	}
	return nil
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "crypto-keygen:", err)
		os.Exit(1)
	}
}
