# Upstream maintenance

The source remote named `upstream` tracks `https://github.com/chatwoot/cli`.
Import upstream changes only through a reviewed integration branch. Preserve the
MIT license, run the Wavo contract and branding gates, and publish releases only
from `origin/main`.

## Prepare a sync

Configure the remotes with `main` as their default branch and keep the upstream
push URL disabled:

```bash
git remote set-url --push upstream no_push
./scripts/sync-upstream.sh
```

The script must start on a clean `main`. It accepts only the exact canonical
HTTPS GitHub URLs for the Wavo origin and upstream CLI repositories and refuses
Git URL rewrite configuration. Network operations ignore caller, global, and
system Git configuration. The script validates both remote default branches,
fetches only their `main` branches with tags disabled, creates the dated
`sync/upstream-YYYYMMDD` branch from `origin/main`, and stages a no-fast-forward
merge. It never commits, pushes, tags, or publishes. A same-day branch collision
is a stop condition; inspect or remove that reviewed branch before trying again.
If Git reports conflicts, the merge is intentionally left open on the sync
branch for manual resolution and review. To abandon it, run `git merge --abort`,
switch back to `main`, and delete only the confirmed sync branch.

When upstream is equal to or already contained by `origin/main`, the script
reports `No upstream changes to stage.` and remains on a clean `main` without
creating a sync branch.

## Required sync evidence

1. Record upstream before and after commits.
2. Refresh the Wavo Swagger snapshot and source SHA.
3. Run unit, race, dev-profile, contract, branding, shellcheck, and release gates.
4. Review help, auth, installer, updater, README, skill, and completion output.
5. Merge through a pull request. Publish no release from the sync branch.
