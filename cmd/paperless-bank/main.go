package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	kongyaml "github.com/alecthomas/kong-yaml"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	// Self-registers "comdirect" in bank.registry via init().
	_ "github.com/jabbrwcky/paperless-bank/internal/bank/comdirect"
)

// PaperlessFlags holds connection settings for the paperless-ngx instance.
type PaperlessFlags struct {
	URL   string `name:"url"   help:"paperless-ngx base URL"  env:"PAPERLESS_URL"`
	Token string `name:"token" help:"paperless-ngx API token" env:"PAPERLESS_TOKEN"`
}

// ComdirectFlags holds credentials for the Comdirect bank integration.
type ComdirectFlags struct {
	ClientID     string `name:"client-id"     help:"OAuth2 client ID"     env:"COMDIRECT_CLIENT_ID"`
	ClientSecret string `name:"client-secret" help:"OAuth2 client secret" env:"COMDIRECT_CLIENT_SECRET"`
	Username     string `name:"username"      help:"Depot-Kennung"         env:"COMDIRECT_USERNAME"`
	Password     string `name:"password"      help:"Online-PIN"            env:"COMDIRECT_PASSWORD"`
	TokenCache   string `name:"token-cache"   help:"Token cache file path" default:"~/.cache/paperless-bank/comdirect-token.json"`
}

// CLI is the root kong command struct.
type CLI struct {
	Paperless PaperlessFlags `embed:"" prefix:"paperless-"`
	Comdirect ComdirectFlags `embed:"" prefix:"comdirect-"`

	Auth AuthCmd `cmd:"" help:"Authenticate with a bank and cache credentials"`
	Sync SyncCmd `cmd:"" help:"Sync new documents from configured banks to paperless-ngx"`
	List ListCmd `cmd:"" help:"List available documents without uploading"`
}

// bankConfig returns the bank.Config for the named bank, or nil if not recognised.
func (c *CLI) bankConfig(name string) *bank.Config {
	switch strings.ToLower(name) {
	case "comdirect":
		return &bank.Config{
			ClientID:     c.Comdirect.ClientID,
			ClientSecret: c.Comdirect.ClientSecret,
			Username:     c.Comdirect.Username,
			Password:     c.Comdirect.Password,
			TokenCache:   c.Comdirect.TokenCache,
		}
	}
	return nil
}

// configuredBanks returns a map of bank name → config for every bank that has
// at minimum a client ID set.
func (c *CLI) configuredBanks() map[string]bank.Config {
	out := make(map[string]bank.Config)
	if c.Comdirect.ClientID != "" {
		out["comdirect"] = bank.Config{
			ClientID:     c.Comdirect.ClientID,
			ClientSecret: c.Comdirect.ClientSecret,
			Username:     c.Comdirect.Username,
			Password:     c.Comdirect.Password,
			TokenCache:   c.Comdirect.TokenCache,
		}
	}
	return out
}

func main() {
	var cli CLI
	k := kong.Must(&cli,
		kong.Name("paperless-bank"),
		kong.Description("Sync bank inbox documents to paperless-ngx."),
		kong.Configuration(kongyaml.Loader, configFile(), "paperless-bank.yaml"),
		kong.UsageOnError(),
	)
	ctx, err := k.Parse(os.Args[1:])
	k.FatalIfErrorf(err)
	k.FatalIfErrorf(ctx.Run(&cli))
}

// configFile returns the path to the YAML configuration file.
// Override via PAPERLESS_BANK_CONFIG environment variable.
func configFile() string {
	if v := os.Getenv("PAPERLESS_BANK_CONFIG"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "paperless-bank", "config.yaml")
}
