package cmd

import (
	"fmt"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/iRaxe/wavo-cli/internal/config"
	"github.com/iRaxe/wavo-cli/internal/output"
	"github.com/iRaxe/wavo-cli/internal/sdk"
)

// App holds shared state passed to every command's Run method.
type App struct {
	Client           *sdk.Client
	Printer          *output.Printer
	Config           *config.Config
	CredentialSource config.CredentialSource
	Version          string
}

// NewApp creates an App from the parsed CLI flags.
// Commands that don't need auth (auth login/logout, config) pass skipAuth=true.
func NewApp(cli *CLI, skipAuth bool, version string) (*App, error) {
	printer := output.NewPrinter(cli.Output, cli.NoColor, cli.Quiet)

	if skipAuth {
		return &App{Printer: printer, CredentialSource: config.CredentialSourceMissing, Version: version}, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg == nil || !cfg.IsValid() {
		return nil, fmt.Errorf("not authenticated. Run '%s auth login' to set up credentials", brand.Command)
	}

	effectiveCfg := *cfg
	if cli.Account > 0 {
		effectiveCfg.AccountID = cli.Account
	}

	apiKey, credentialSource, err := config.ResolveAPIKey(&effectiveCfg)
	if err != nil {
		return nil, fmt.Errorf("not authenticated: %w", err)
	}

	client := sdk.NewClient(
		effectiveCfg.BaseURL,
		apiKey,
		effectiveCfg.AccountID,
		sdk.WithVerbose(cli.Verbose),
	)

	return &App{
		Client:           client,
		Printer:          printer,
		Config:           cfg,
		CredentialSource: credentialSource,
		Version:          version,
	}, nil
}
