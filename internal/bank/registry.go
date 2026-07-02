package bank

import "fmt"

// Config holds credentials common to all bank integrations.
// Each bank uses the fields relevant to its auth mechanism.
type Config struct {
	ClientID     string
	ClientSecret string
	Username     string
	Password     string
	// TokenCache is the path to the on-disk token cache file.
	// Supports ~ expansion.
	TokenCache string
}

// Factory creates a DocumentSource from the given config.
type Factory func(Config) (DocumentSource, error)

var registry = map[string]Factory{}

// Register is called by each bank package's init() to self-register.
func Register(name string, f Factory) {
	registry[name] = f
}

// New creates a DocumentSource for the named bank.
func New(name string, cfg Config) (DocumentSource, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown bank %q; registered: %v", name, Names())
	}
	return f(cfg)
}

// Names returns all registered bank names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}
