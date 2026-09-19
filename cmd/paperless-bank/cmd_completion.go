package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/miekg/king"
)

// CompletionCmd prints a shell completion script for paperless-bank to
// stdout, generated from the CLI's kong definition via
// github.com/miekg/king.
type CompletionCmd struct {
	Shell string `arg:"" enum:"bash,zsh,fish" help:"Shell to generate a completion script for (bash, zsh, fish)."`
}

func (c *CompletionCmd) Run(k *kong.Kong) error {
	var gen king.Completer
	switch c.Shell {
	case "bash":
		gen = &king.Bash{}
	case "zsh":
		gen = &king.Zsh{}
	case "fish":
		gen = &king.Fish{}
	default:
		// Unreachable: kong's enum tag rejects any other value before Run is called.
		return fmt.Errorf("unsupported shell %q", c.Shell)
	}

	gen.Completion(k.Model.Node, "")
	_, err := os.Stdout.Write(gen.Out())
	return err
}
