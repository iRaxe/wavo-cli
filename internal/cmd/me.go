package cmd

// MeCmd is `wavo me` — alias of `wavo auth status`.
type MeCmd struct{}

func (c *MeCmd) Run(app *App) error { return runAuthStatus(app) }
