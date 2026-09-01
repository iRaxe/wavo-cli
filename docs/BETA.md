# Wavo CLI beta onboarding

Wavo CLI is pre-release software intended for a limited, announced beta. Beta
operators need a Wavo user with the least-privileged workspace role sufficient
for their work and a corresponding personal API token. Desktop logins use the
operating-system keyring; headless servers use a process environment credential.

Release archives are available for:

- Linux on x86-64 and arm64 (`.tar.gz`)
- macOS on x86-64 and Apple silicon (`.tar.gz`)
- Windows on x86-64 and arm64 (`.zip`)

## Install and verify

Install on Linux or macOS and complete the interactive login:

```bash
curl -fsSL https://my.wavo.it/install-cli | env WAVO_VERSION=v0.1.0-beta.2 sh
wavo auth login
wavo config view
wavo convs --assignee me -s open -o json
```

This command pins the announced `v0.1.0-beta.2` tag because the stable `latest`
release channel does not select prereleases. Use the tag from the beta
announcement. A later announced stable release may use the unpinned installer
channel.

Confirm the installed build and command grammar before using a customer
workspace:

```bash
wavo version
wavo --help
wavo convs --help
```

`wavo config view` is the pure-read preflight: it confirms the Wavo instance,
account ID, and credential source without exposing the token or changing local
state. The final `convs` command is a pure-read smoke test. Review its account
and output; do not send a reply or change state during initial verification.

When an explicit identity check is needed, run:

```bash
wavo auth status
```

This command also verifies the current user and workspace. With a keyring-backed
login, it may refresh the nonsecret cached `user_id` in `~/.wavo/config.yaml`.
That documented local metadata refresh means `auth status`, `me`, and `whoami`
are identity checks, not pure-read preflight commands.

## Credential boundary

Interactive login stores the Wavo token in the operating-system keyring. The
files under `~/.wavo` contain nonsecret local state: base URL, workspace ID,
cached user ID, help-center defaults, and process locks. The token must never be
written to `~/.wavo/config.yaml`.

`WAVO_API_KEY` overrides the saved keyring credential for headless execution.
Use it only in a secret manager or tightly scoped process environment. Do not
export it in a shared shell profile, echo it, put it on a command line, enable
shell tracing, or include it in captured diagnostics.

The keyring credential is bound to the Wavo instance and workspace selected at
login. `--account` cannot switch a normal keyring login to another workspace;
run `wavo auth login` and select that workspace instead. For headless use,
combine `--account` only with a separately verified `WAVO_API_KEY` authorized
for the exact target workspace. Never reuse IDs, content, or API data from one
workspace in another.

Never paste a Wavo token into a prompt, ticket, chat message, command argument,
or diagnostic report.

On a server without a keyring, load the token without putting it in shell
history and use the supported headless login:

```bash
read -rsp "Wavo API Key: " WAVO_API_KEY
export WAVO_API_KEY
echo
wavo auth login --headless
```

This validates the user and selected workspace, then writes only `base_url`,
`account_id`, and cached `user_id` to `~/.wavo/config.yaml`. It never writes the
token. The environment variable must remain set for Wavo commands in that shell;
run `unset WAVO_API_KEY` when finished. Do not put it in `.bashrc` or another
plaintext profile.

## Revoke local access

Run logout before removing local state:

```bash
wavo auth logout
rm -rf ~/.wavo
```

`wavo auth logout` removes the saved credential from the operating-system
keyring and removes the active config file. Only after logout succeeds should
you remove `~/.wavo`; that second command removes remaining nonsecret local
state, such as locks and profile-specific configuration. It is not a substitute
for keyring deletion. If `WAVO_API_KEY` is set, unset or revoke it separately.

On Windows PowerShell, run logout first, then remove the nonsecret local state
only if it still exists:

```powershell
wavo auth logout
if (Test-Path "$HOME\.wavo") {
  Remove-Item -Recurse -Force "$HOME\.wavo"
}
```

Filesystem cleanup never replaces logout, keyring deletion, or account-side
token revocation.

For a lost or unavailable device, open the Wavo UI from a trusted device and
revoke or rotate the corresponding personal API token in the account settings.
Local logout cannot revoke a token on a device you no longer control.

## Verify a Windows download

Set the announced beta tag and version explicitly. The tag includes `v`; the
archive version does not. These commands illustrate the x86-64 asset;
use `arm64` in the archive name on Windows arm64.

```powershell
$BetaTag = "v0.1.0-beta.2"
$BetaVersion = "0.1.0-beta.2"
$Archive = "wavo_${BetaVersion}_Windows_x86_64.zip"
$Base = "https://github.com/iRaxe/wavo-cli/releases/download/$BetaTag"
Invoke-WebRequest "$Base/$Archive" -OutFile $Archive
Invoke-WebRequest "$Base/checksums.txt" -OutFile "checksums.txt"
$Line = (Get-Content "checksums.txt") |
  Where-Object { $_ -match "\s$([regex]::Escape($Archive))$" } |
  Select-Object -First 1
if (-not $Line) { throw "$Archive is missing from checksums.txt" }
$Expected = $Line.Split()[0]
$Actual = (Get-FileHash -Algorithm SHA256 $Archive).Hash.ToLowerInvariant()
if (-not $Expected -or $Actual -ne $Expected.ToLowerInvariant()) {
  throw "SHA-256 checksum verification failed"
}
Expand-Archive $Archive -DestinationPath ".\wavo"
.\wavo\wavo.exe version
.\wavo\wavo.exe --help
```

Use the tag named in the beta announcement; do not infer an unannounced
"latest" release. Stop if the archive is absent from `checksums.txt` or the
hash differs.

## Support evidence and beta stop conditions

When reporting a beta problem, include only:

- `wavo version` output
- operating system and architecture
- command name, without token or customer-supplied arguments
- exit code
- the smallest useful, redacted error excerpt

Do not include conversation, contact, or message bodies unless their inclusion
is deliberately minimized and specifically authorized. Remove names, email
addresses, phone numbers, tokens, workspace identifiers, and unrelated API
fields wherever they are not essential to reproduce the issue.

Stop using the beta and contact the announced Wavo support channel if checksum
verification fails, authentication appears scoped to the wrong workspace, a
pure-read command changes state, output exposes a credential, or a mutation has
an ambiguous result. The documented nonsecret `user_id` refresh by `auth status`,
`me`, or `whoami` is local metadata maintenance, not a pure-read command. Do not
retry an ambiguous mutation. Send no tokens; support does not need them.
