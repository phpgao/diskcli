package cli

import (
	"fmt"

	"github.com/phpgao/diskcli/internal/config"
	"github.com/phpgao/diskcli/internal/provider"
	"github.com/phpgao/diskcli/internal/quark"
)

// newProvider creates a provider instance based on the given name and config.
//
// Only quark is supported currently; baidu/aliyun/onedrive will be added here
// as new cases when implemented. Returns an error if the required credential
// is missing (surfaced when a command actually needs the provider).
func newProvider(name string, cfg *config.Config, customHeaders map[string]string) (provider.Provider, error) {
	if cfg.QuarkCookie == "" {
		return nil, fmt.Errorf("quark cookie not provided: set --qk-cookie, %s env, or qk_cookie in %s",
			config.EnvQuarkCookie, cfg.ConfigPath)
	}
	switch name {
	case "quark", "":
		client, err := quark.NewClient(cfg.QuarkCookie, quark.WithCustomHeaders(customHeaders))
		if err != nil {
			return nil, err
		}
		return quark.NewAdapter(client), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s (only quark is supported currently)", name)
	}
}
