# Wavo CLI

Manage Wavo conversations, contacts, assignments, and help-center content from
the terminal.

## Install

```bash
curl -fsSL https://my.wavo.it/install-cli | env WAVO_VERSION=v0.1.0-beta.1 sh
wavo auth login
```

The installer URL and release assets become available with the announced beta
release. Until that announcement, treat these commands as the beta install flow,
not as a claim that a public installer is already live.
The beta command pins its announced prerelease tag because the stable `latest`
channel does not select prereleases. A later announced stable release may use
the unpinned installer channel.

Interactive login validates the selected Wavo workspace and stores the token
only in the operating-system keyring. `~/.wavo/config.yaml` contains nonsecret
settings such as the base URL, workspace ID, and help-center defaults; it never
contains the token. See the [beta onboarding guide](docs/BETA.md) for supported
platforms, verification, revocation, and support guidance.

Use `wavo config view` as the nonmutating preflight before workspace reads.
`wavo auth status` and its `me`/`whoami` aliases are explicit identity checks;
with a keyring login they may refresh the nonsecret cached user ID in local
config, so they are not pure-read preflight commands.

## AI-agent skill

Install the bundled Wavo skill after the beta package is announced:

```bash
npx skills add iRaxe/wavo-cli
```

Agents may perform pure-read inspection within the user's requested workspace
after confirming it with `wavo config view`, without write approval. Every reply
or private note, assignment or unassignment, status or snooze change, label
change, priority change, contact mutation, or mutating raw API call requires
explicit approval immediately before execution. The agent must show the exact
workspace, target, effect, and command before that approval; an earlier or
blanket approval is not enough.

## Command grammar

Wavo uses noun-first, ID-first commands:

- A plural noun lists resources: `wavo convs`, `wavo contacts`, `wavo agents`.
- A singular noun plus ID views one resource: `wavo conv 123`.
- A singular noun, ID, then verb acts on it: `wavo conv 123 reply "Thanks"`.

```bash
wavo convs                                  # Your open conversations
wavo convs --assignee all --inbox 5         # Open conversations in inbox 5
wavo convs --query "refund" -o json         # Search message content

wavo conv 123                               # View a conversation
wavo conv 123 messages -o json              # Read its messages
wavo conv 123 reply "Looking into it"
wavo conv 123 reply "Internal context" --private
wavo conv 123 resolve                       # Also: open, pending, snooze
wavo conv 123 snooze --until 7d
wavo conv 123 assign --agent me             # Or: --agent 42, --team 7
wavo conv 123 unassign
wavo conv 123 label billing,urgent          # Replaces existing labels
wavo conv 123 priority urgent               # urgent|high|medium|low|none

wavo contacts --search "customer@example.com" -o json
wavo contact 456
wavo contact 456 conversations -o json

wavo inboxes -o json
wavo inbox 5 -o json
wavo agents -o json
wavo labels -o json
wavo teams -o json

wavo hcs -o json
wavo hc default                             # Show the local default
wavo hc articles --portal support --locale en --query "account" -o json
wavo hc article create-an-account --portal support -o json

wavo api /conversations/123                 # GET, account-relative
wavo api --exact /api/v1/profile            # GET, exact path
wavo api -X PATCH /conversations/123 --data '{"priority":"high"}'
```

Run `wavo --help` or `wavo <command> --help` for the generated reference.

## Output and scripting

Text is the human-readable default. Use `-o json` for full-fidelity structured
output, `-o csv` for tables, and `-q` for ID-only list output. The CLI never
automatically switches formats when piped. Successful commands exit `0`; errors
are written to stderr and return a nonzero exit code.

```bash
wavo convs --assignee me -s open -o json \
  | jq '.data.payload[] | {id, status}'
wavo contacts --search "customer@example.com" -o json \
  | jq '.payload[] | {id, name}'
wavo convs --assignee all -s pending -q
```

`WAVO_API_KEY` is available as a headless override, but environment credentials
must never be placed in command arguments, prompts, logs, or shell tracing.
Never paste a Wavo token into a prompt, ticket, chat message, command argument,
or diagnostic report.

A keyring credential is bound to the Wavo instance and workspace selected at
login; `--account` cannot switch that normal login to another workspace. Run
`wavo auth login` and select the other workspace instead. In headless use,
`--account` is allowed only with a separately verified `WAVO_API_KEY` authorized
for that exact workspace. Never carry IDs or returned data between workspaces.

## Project information

- [Beta onboarding and support](docs/BETA.md)
- [Third-party notices](NOTICE.md)
- [MIT license](LICENSE)
