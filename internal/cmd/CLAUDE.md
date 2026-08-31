# internal/cmd - Wavo command layer

Kong command definitions live here. Each command is a struct of flags and
arguments with a `Run(app *App) error` method. `CLI` in `cli.go` is the canonical
command tree; generated `wavo --help` is the canonical public reference.

## Public grammar

The Wavo CLI is noun-first and ID-first:

```bash
wavo convs
wavo conv 123
wavo conv 123 messages
wavo conv 123 reply "hello"
wavo contact 456 conversations
```

Kong internally parses singular commands as verb then ID. `cmd/wavo/main.go`
rewrites the public `<noun> <id> <verb>` form before parsing and rewrites help
back to the public form. Keep its verb tables in sync when adding singular
commands.

## Command tree

- Lists: `convs`, `contacts`, `inboxes`, `agents`, `labels`, `teams`, `hcs`.
- Conversation reads: `conv <id>`, `messages`, `contact`.
- Conversation writes: `reply`, `resolve`, `open`, `pending`, `snooze`,
  `assign`, `unassign`, `label`, `priority`.
- Contact reads: `contact <id>`, `contact <id> conversations`.
- Help centers: `hc list`, `hc default`, `hc articles`, `hc article`.
- Identity and setup: `me`, `whoami`, `auth`, `config`, `completion`, `version`.
- Escape hatch: `api <path>` for an authenticated raw Wavo API request.

Conversation writes use `withConvLock` and fail fast when another Wavo process
is mutating the same conversation. Do not bypass that lock for new conversation
mutations.

## App and output

`App` provides the SDK client, printer, config, and version. Root flags are
`--output/-o`, `--account/-a`, `--quiet/-q`, `--no-color`, and `--verbose/-v`.
Text is the human default; JSON preserves SDK response structures; CSV applies
to table output; quiet list output emits the first column, normally IDs.

`auth`, `config`, `completion`, `version`, `me`, and `whoami` bypass ordinary
authenticated client construction. `auth status`, `me`, and `whoami` share the
same status implementation. `WAVO_API_KEY` overrides the OS keyring for
headless use; never write it to YAML or logs.

## Kong pattern

```go
type MyCmd struct {
  ID int `arg:"" help:"Resource ID."`
}

func (c *MyCmd) Run(app *App) error {
  return nil
}
```

Add targeted command and parser tests for grammar, side effects, output shape,
and failure behavior. Use Wavo-only fixture strings in customer-visible output.
