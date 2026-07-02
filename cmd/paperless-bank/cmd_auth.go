package main

import (
	"context"
	"fmt"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

// AuthCmd authenticates with a bank and persists the session token to disk.
type AuthCmd struct {
	Bank string `arg:"" help:"Bank to authenticate with (e.g. comdirect)" default:"comdirect"`
}

func (a *AuthCmd) Run(cli *CLI) error {
	cfg := cli.bankConfig(a.Bank)
	if cfg == nil {
		return fmt.Errorf("no configuration found for bank %q", a.Bank)
	}

	src, err := bank.New(a.Bank, *cfg)
	if err != nil {
		return err
	}

	auth, ok := src.(bank.Authenticator)
	if !ok {
		return fmt.Errorf("%s does not support interactive authentication", a.Bank)
	}

	return auth.Authenticate(context.Background())
}
