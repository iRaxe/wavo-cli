# Wavo CLI

CLI for [Wavo](https://my.wavo.it) — manage conversations, send replies, and run common workflows from your terminal.

## Install

**macOS / Linux** — install script (detects OS/arch, fetches the matching release binary, verifies SHA256):

```bash
curl -fsSL https://my.wavo.it/install-cli | sh
```

For a specific version or Windows, see the [install docs](https://github.com/iRaxe/wavo-cli/releases).

## Setup

```bash
wavo auth login
```

You'll be prompted for your **Base URL**, **API Key**, and **Account ID**. Credentials are validated before saving. Non-secret config lives at `~/.wavo/config.yaml`; the API key is stored in your OS keyring. For CI or headless environments, set `WAVO_API_KEY` to override the keyring.

## Agent Skill

If you use Claude Code, Cursor, or another AI coding assistant, install the agent skill so it knows the CLI's grammar and safety rules before sending customer-visible replies:

```bash
npx skills add iRaxe/wavo-cli              # current project
npx skills add iRaxe/wavo-cli --global     # all projects
```

See the [agent skill docs](https://github.com/iRaxe/wavo-cli) for details.

## Usage

The CLI uses a simple noun grammar:

- **Plural noun = list:** `wavo convs`, `wavo contacts`, `wavo agents`
- **`<noun> <id>`** views: `wavo conv 123`
- **`<noun> <id> <verb>`** acts: `wavo conv 123 reply "thanks"` — id before verb, the way you'd say it.

```bash
wavo convs                                 # Open conversations assigned to you
wavo convs --assignee all --inbox 5        # All conversations in inbox 5
wavo convs --query "refund"                # Search by message content

wavo conv 123                              # View
wavo conv 123 reply "Looking into it"
wavo conv 123 reply "internal note" --private
wavo conv 123 resolve                      # Or: open, pending, snooze
wavo conv 123 assign --agent me            # Or: --agent alice, --agent 42, --team 7
wavo conv 123 label billing,urgent
wavo conv 123 priority urgent              # urgent | high | medium | low | none

wavo contacts --search "john"
wavo contact 456 conversations

wavo inboxes / agents / labels / teams     # List
wavo me                                    # Your profile

wavo hcs                                   # List help centers
wavo hc default wavo-support       # Save default help center and locale
wavo hc articles --query "account"
wavo hc articles --category getting-started
wavo hc article create-a-agent-bot
wavo hc articles --portal other-help-center --locale fr

wavo api /conversations/123                # Expands to /api/v1/accounts/<id>/conversations/123
wavo api -X PATCH /conversations/123 --data '{"status":"open"}'
```

Run `wavo --help` or see the [full command reference](https://github.com/iRaxe/wavo-cli).

## Output Formats

`-o text` (default), `-o json`, `-o csv`, or `-q` (IDs only, for scripting):

```bash
wavo convs -o json | jq '.data.payload[].id'
wavo convs -q | xargs -I{} wavo conv view {}
```

For piping, batching, and CI workflows, see the [scripting guide](https://github.com/iRaxe/wavo-cli).

## License

MIT
