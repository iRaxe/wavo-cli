package cmd

// WhoamiCmd is `wavo whoami` — alias of `wavo auth status`.
type WhoamiCmd struct{}

func (c *WhoamiCmd) Run(app *App) error { return runAuthStatus(app) }
