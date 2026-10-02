# Publishing releases

The installer and updater use stable GitHub Releases in
`TobiasCoding/ShellStudio`. They do not use the latest commit on `main` as a
version. Public installation requires public access to the repository, the raw
installer on `main`, and the release assets.

## First release

A successful push to `main` runs Verify and build and stores build artifacts in
Actions. It does not publish a GitHub Release. Until the first stable release is
published, `/releases/latest` redirects to the empty releases page and the public
installer has no binary to download.

After merging the changes and confirming CI passes, publish the current version
from the reviewed `main` commit:

```sh
git tag v0.2.0
git push origin v0.2.0
```

This starts Publish release. Wait for that workflow to succeed and for the
release page to show both Linux binaries and `SHA256SUMS`, then retry the same
curl installer command. A tag by itself or a source-only release is insufficient.
If the workflow fails, resolve its reported error before retrying; do not mark a
draft latest until every required asset is uploaded.

## Subsequent releases

1. Update the default version in `Makefile` and `cmd/shellstudio/main.go`.
2. Review and merge the source, installer, and release workflow onto `main`.
3. Run `make check` locally. Review the publication scan and generated source.
4. Create and push a stable tag, for example `v0.2.0`, pointing at that commit.
5. The Publish release workflow repeats verification, builds Linux amd64/arm64,
   uploads all artifacts into a draft, then publishes it as the latest release.
6. Verify the one-line installer in a clean account and check `shellstudio update
   --check` from the preceding version.

Tagging and pushing intentionally publishes a release after CI succeeds. The
workflow requires GitHub Actions with contents-write permission; no external
server, package registry, personal access token or committed credential is needed.
A failed upload leaves a draft, not a partially published update. Inspect or
remove that draft before retrying the tag workflow. Never replace the assets of
an existing published version: fix the issue in a new version.

Required assets, all from the same tag:

- `shellstudio-linux-amd64`
- `shellstudio-linux-arm64`
- `SHA256SUMS` (sha256sum format with bare asset filenames)
- `shellstudio-VERSION-source.tar.gz`, including third-party licenses
- `LICENSE` and `THIRD_PARTY_NOTICES.md`

Build locally with `make release VERSION=0.2.0`. Install without GitHub using
`sh scripts/install.sh --from "$PWD/dist"`. CI tests installers using isolated
homes and simulated HTTPS release downloads; updater tests use an isolated TLS
server and real binaries. No tests publish a release or modify user workspaces.

GitHub determines the latest published stable release; ShellStudio additionally
compares the numeric major/minor/patch version and refuses automatic downgrades
and prereleases. API failures, rate limits, missing assets and offline hosts
leave the current executable usable. The startup check can be disabled with
`SHELLSTUDIO_NO_UPDATE_CHECK=1`; manual `shellstudio update` remains available.

Reference: [GitHub Releases API](https://docs.github.com/en/rest/releases/releases#get-the-latest-release)
and [GitHub CLI release creation](https://cli.github.com/manual/gh_release_create).
