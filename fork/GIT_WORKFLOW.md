# Git workflow for the `jecruz/ollama` fork

This fork maintains two parallel build targets off the same codebase:

| Target | Branch | Min macOS | Architecture | Bundle id | Build script | Build artifact |
|---|---|---|---|---|---|---|
| Apple Silicon (tracks upstream) | `main` | 14.0 | universal (arm64+amd64) | `com.electron.ollama` | `scripts/build_darwin.sh` | `dist/Ollama.dmg` |
| Intel macOS 12+ community build | `intel-macos12` | 12.0 | amd64-only | `com.electron.ollama.intel` | `scripts/build_darwin_intel.sh` | `dist/Ollama-intel-macos12-<ver>.dmg` |

The `intel-macos12` branch carries the JSC lookbehind polyfill, the code-split UI, the `LSMinimumSystemVersion=12.0` bundle metadata, and a single-arch build script. The two branches share the same `main` ancestry and are re-synced via rebase.

---

## Remotes

```sh
origin    https://github.com/jecruz/ollama.git       # this fork
upstream  https://github.com/ollama/ollama.git       # canonical source
```

Both should be present. If `upstream` is missing:

```sh
git remote add upstream https://github.com/ollama/ollama.git
git fetch upstream
```

---

## Branching model

```
upstream/main ──► origin/main ──► origin/intel-macos12 ──► tag: intel-macos12-1
       │                │                    │
       │                │                    └─ fix → intel-macos12-2 → …
       │                │
       └───── sync ─────┘   (no work happens on origin/main directly; it just
                            tracks upstream via fast-forward merge or rebase)
```

- **`main`** — clean mirror of upstream; no direct commits. Sync only.
- **`intel-macos12`** — long-lived branch holding the Intel-specific patches. New work for Intel happens here, rebased on top of upstream.
- **Tags** — `intel-macos12-N` for each verified build. Move forward, never re-tag.

---

## Routine syncs

### Sync `main` with upstream (no work in this branch)

```sh
git checkout main
git fetch upstream
git merge --ff-only upstream/main
git push origin main
```

If the fast-forward fails, something is wrong — do not force-push.

### Sync `intel-macos12` with upstream

```sh
git checkout intel-macos12
git fetch upstream
git rebase upstream/main
# resolve any conflicts (see "Conflict hot spots" below), then:
git push --force-with-lease origin intel-macos12
```

Rebase (not merge) so the Intel commits stay linearly on top of upstream and cherry-picking stays simple.

---

## Cherry-picking upstream fixes into the Intel branch

Most upstream PRs land cleanly on `intel-macos12` because the Intel branch only diverges in a small surface area (`app/cmd/app/*`, `app/ui/app/src/{api.ts,main.tsx,routes/*,components/*,types/webview.d.ts}`, `vite.config.ts`, `app/darwin/Ollama.app/Contents/Info.plist`, `scripts/build_darwin_intel.sh`).

To bring a specific upstream commit `abc1234` into the Intel branch:

```sh
git checkout intel-macos12
git fetch upstream
git cherry-pick -x abc1234
# If conflicts: git status, fix, git add, git cherry-pick --continue
git push --force-with-lease origin intel-macos12
```

The `-x` flag records the upstream commit hash in the message so the lineage is auditable.

### Cherry-pick a range

```sh
git cherry-pick -x upstream/main~5..upstream/main
```

### Cherry-pick a PR by merge commit

If upstream squash-merged a PR, the merge commit is the one to pick:

```sh
git fetch upstream pull/1234/head:pr-1234
git cherry-pick -x pr-1234
```

Then drop the temp ref:

```sh
git branch -D pr-1234
```

---

## Cherry-picking Intel fixes back upstream

If a change on `intel-macos12` is genuinely beneficial to everyone (not just Intel) — for example, a memory-pressure fix, a fetch hardening, or a UI improvement — propose it back to upstream:

1. Identify the commit on `intel-macos12` (e.g. `7ab68264`).
2. Re-apply it on a fresh branch off `upstream/main`:

   ```sh
   git fetch upstream
   git checkout -b fix/<short-description> upstream/main
   git cherry-pick -x 7ab68264
   ```

3. Resolve any conflicts that arise because `intel-macos12` carries extra changes.
4. Open a PR against `upstream/main`.

Do not push Intel-specific changes (`app/darwin/Ollama.app/Contents/Info.plist` `LSMinimumSystemVersion`, the Intel build script, the lookbehind polyfill) upstream — those are Mac-12-only and would break Apple-Silicon / macOS 14+ users.

---

## Conflict hot spots

When rebasing `intel-macos12` onto a new `upstream/main`, conflicts are most likely in:

| File | Why | Resolution |
|---|---|---|
| `app/cmd/app/webview.go` | Upstream regularly refactors the webview init; the polyfill block sits inside the init script | Move the polyfill IIFE back inside the new init script (search for the `// Work around a JavaScriptCore limitation` comment) |
| `app/ui/app/vite.config.ts` | Upstream may add or remove plugins | Re-apply `TanStackRouterVite({ target: "react", autoCodeSplitting: true })` after the merge |
| `app/ui/app/src/api.ts` | New endpoints get added; `credentials: "include"` is the Intel-side convention | When porting new fetch calls, mirror the `credentials: "include"` pattern |
| `app/ui/app/src/main.tsx` | Upstream may change bootstrap ordering | Keep the `AppReady` mount inside the provider tree and the `.catch(() => {})` on `fetchUser()` |
| `app/ui/app/src/routes/index.tsx` | Upstream may change the home redirect logic | Keep the 5s fallback `setTimeout` so the chat UI never gets stuck on `/` |
| `app/darwin/Ollama.app/Contents/Info.plist` | Upstream bumps Xcode/DTSDK keys | Re-apply `CFBundleDisplayName=Ollama Intel`, `CFBundleName=Ollama Intel`, `CFBundleIdentifier=com.electron.ollama.intel`, `LSMinimumSystemVersion=12.0` |

For the Info.plist, the easiest is to do the edits via `plutil` after the rebase:

```sh
plutil -replace CFBundleDisplayName -string "Ollama Intel" \
       app/darwin/Ollama.app/Contents/Info.plist
plutil -replace CFBundleName -string "Ollama Intel" \
       app/darwin/Ollama.app/Contents/Info.plist
plutil -replace CFBundleIdentifier -string "com.electron.ollama.intel" \
       app/darwin/Ollama.app/Contents/Info.plist
plutil -replace LSMinimumSystemVersion -string "12.0" \
       app/darwin/Ollama.app/Contents/Info.plist
```

---

## Cutting a new Intel release

```sh
git checkout intel-macos12
# ensure tree is clean, rebased, and the current HEAD builds
./scripts/build_darwin_intel.sh
# smoke-test the produced /Applications/Ollama Intel.app
# then tag and push:
NEW=2  # bump from intel-macos12-1 → intel-macos12-2
git tag -a "intel-macos12-${NEW}" -m "<summary of what changed>"
git push origin "intel-macos12-${NEW}"

gh release create "intel-macos12-${NEW}" \
    --repo jecruz/ollama \
    --title "Ollama Intel macOS 12 — <short summary>" \
    --notes-file - \
    dist/Ollama-intel-macos12-${VERSION}.dmg < RELEASE_NOTES.md
```

Never reuse or move an existing tag. Always create a new one.

---

## What does NOT go on `intel-macos12`

- Upstream-only changes that have nothing to do with the Intel / macOS 12 workaround. Cherry-pick those into a topic branch off `upstream/main` and PR them.
- Build artifacts (`dist/`, `build/`, `build-intel/`, `ollama-debug`, the produced DMG). These are gitignored already — do not untrack them just to commit a binary.
- Personal `.DS_Store`, editor swap files, secrets. Standard `.gitignore` covers most of this.

---

## Quick reference

```sh
# What branch am I on?
git branch --show-current

# What does my Intel branch have that upstream doesn't?
git log --oneline upstream/main..intel-macos12

# What's still unmerged from upstream?
git log --oneline intel-macos12..upstream/main

# When did my Intel branch last sync with upstream?
git log --oneline -1 intel-macos12 ^upstream/main
# (empty output = up to date)

# Roll back the most recent Intel commit (keep changes in working tree)
git reset --mixed HEAD~1

# Amend the most recent Intel commit
git commit --amend

# Drop a bad tag locally + remote
git tag -d intel-macos12-2
git push origin :refs/tags/intel-macos12-2
```
