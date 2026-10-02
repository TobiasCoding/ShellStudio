# Development, previews and releases

The installer and updater use the latest **published stable GitHub Release** in
`TobiasCoding/ShellStudio`. A commit, local build, CI artifact or tag alone does
not notify users. `VERSION` is the single stable version source; direct `go build`
uses `dev`. Normal pushes and pull requests run CI without publishing releases.

## Try changes before releasing

```sh
make preview
```

This builds the current working tree (including uncommitted changes) with a
`VERSION-dev.COMMIT` label and launches it with separate config, database, state
and tmux sockets under `.work/preview/`. It neither replaces the installed binary
nor checks for updates. Preview state persists between launches, so you can try
F2/F3 menus, drag and drop, notes and console reconnects before publishing.
Commands you run inside preview consoles still run as your user; this is data
isolation, not a security sandbox. `make clean` removes preview state as well.

Use `python3 scripts/release.py preview --build-only` to build without launching.
Launch through `make preview` to ensure the separate environment is always set.

Run the complete automated suite without packaging, changing the version,
creating tags, pushing or notifying users:

```sh
python3 scripts/release.py check
```

`make check` is equivalent. It includes Go tests, race detection, real terminal
interactions, offline MCP, installer tests, release pipeline tests, vet and the
publication audit. Commit reviewed new files so the audit includes them.
CI also saves a commit-labelled candidate executable as an Actions artifact.
Those artifacts are for testing, not update distribution.

## Publish a verified release

Review and commit the source changes on `main`, then run:

```sh
make publish
# Optional: python3 scripts/release.py publish --bump minor
# Optional: python3 scripts/release.py publish --bump major
```

Requirements: Git push access to `origin`, Go, Python 3, make and tmux. No local
GitHub CLI or stored API token is required. The default bump is `patch`.

The script:

1. Requires a clean `main`, fetches tags and refuses a branch behind `origin/main`.
2. Audits tracked source and every outgoing commit, including files later deleted.
3. Computes the next numeric version from `VERSION` and all stable tags.
4. Freezes the candidate in an ignored directory and runs the full suite there.
5. Builds both Linux architectures, checks ELF headers, audits the source archive
   and generates checksums. A failed check leaves the version and Git refs alone.
6. Confirms source stayed unchanged, commits `VERSION`, creates an annotated tag
   and atomically pushes `main` plus that tag. It never force-pushes.
7. GitHub's **Publish release** workflow verifies tag/version agreement, repeats
   tests and builds, uploads all required files to a draft, then publishes latest.

Wait for **Publish release** to succeed. The local script reports a successful
push, not a successful remote release. Existing users see the offer at their next
interactive startup, or through `shellstudio update --check`; installation still
requires their acceptance. A running workspace is not interrupted.

If a push fails, the script prints the exact retry command; the verified local
commit and tag remain. If CI fails, no update is published. Fix code with a new
patch release; never move a public tag or replace published binaries. An upload
failure may leave a draft: inspect/remove that draft before rerunning its workflow.

## Publication boundaries

`.gitignore` excludes local databases, logs, environment files, keys, sockets,
runtime folders, `.work/`, `bin/` and `dist/`. The publication audit additionally
rejects prohibited tracked paths, symlinks, unexpected binaries and recognized
credential patterns. It also checks outgoing history before the automated push.
These checks cannot recognize every possible personal datum: review staged
changes, and keep personal experiments in `.work/` or outside the repository.

Packaging uses the audited **Git file list**, never recursive directory inclusion.
An untracked personal file inside `docs/` or `tests/` cannot enter the source
archive. Exported source contains its own file manifest for rebuilding without
Git. Third-party source and licenses are included. No test fixtures from `.work/`
are published. GitHub's automatically generated source downloads contain the
tagged tracked tree, which is subject to the same publication gate.

Required release assets:

- `shellstudio-linux-amd64` and `shellstudio-linux-arm64`
- `SHA256SUMS`
- `shellstudio-VERSION-source.tar.gz`
- `LICENSE` and `THIRD_PARTY_NOTICES.md`

`make release` packages the current `VERSION` locally without creating a public
release. For an offline installation use `sh scripts/install.sh --from "$PWD/dist"`.
The installer validates checksums, architecture and version before replacement.

GitHub supplies the latest stable release; ShellStudio refuses automatic
downgrades and prereleases. Network/API failures leave the installed program
usable. `SHELLSTUDIO_NO_UPDATE_CHECK=1` disables the startup check.

References: [GitHub release management](https://docs.github.com/en/repositories/releasing-projects-on-github/managing-releases-in-a-repository)
and [GitHub Releases API](https://docs.github.com/en/rest/releases/releases#get-the-latest-release).
