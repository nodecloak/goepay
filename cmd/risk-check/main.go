package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"goepay/internal/bootstrap"
	"os"
	"time"
)

func main() {
	once := flag.Bool("once", false, "run one bounded original core risk check")
	key := flag.String("external-no", "", "stable original run identifier; reuse only to read the same completed result")
	flag.Parse()
	if !*once || os.Getenv("GOEPAY_ENABLE_CORE_RISK_CHECK") != "true" || *key == "" {
		fmt.Fprintln(os.Stderr, "core risk check requires explicit enable, --once and --external-no")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, store, e := bootstrap.Connect(ctx)
	if e != nil {
		fmt.Fprintln(os.Stderr, "core risk check startup failed")
		os.Exit(1)
	}
	defer pool.Close()
	result, e := store.RunCoreRiskCheck(ctx, *key)
	if e != nil {
		fmt.Fprintln(os.Stderr, "core risk check could not be confirmed")
		os.Exit(1)
	}
	if e = json.NewEncoder(os.Stdout).Encode(result); e != nil {
		os.Exit(1)
	}
}
