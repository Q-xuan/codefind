# v0.2.0-rc.1 Local Release Checklist

This is a local prerelease closeout, not authorization to publish a Git tag or remote release.

## Local gates

- [x] Review default text compatibility and new XLSX/read result shapes; see docs/json-contract.md.
- [x] Add real-executable E2E: search unknown position → read returned coordinate → verify source hash unchanged.
- [x] Preserve formula/cache distinction, partial results and bounded readback; regression-test malformed arguments and source line directives.
- [x] Set producer version to 0.2.0-rc.1 and separate the changelog from the previous baseline.
- [x] Final go test ./..., go vet ./..., formatting/diff checks.
- [x] Build Windows amd64 package, verify manifest/hash and packaged-binary E2E.
- [x] Cross-compile Linux amd64 and macOS arm64 (not equivalent to native runtime tests).
- [x] Copy verified package binary to 下游私有项目/cli/codefind and validate text Shadow adapter.
- [x] Validate updated Excel skill entrypoint and existing catalog tests.
- [x] Scan public package inputs for private paths/credentials; include synthetic tests only.

## Packaging

Run `pwsh -File tools/package.ps1` from the repository. It refuses to overwrite an existing version directory/archive. Package includes executable, docs, license, source snapshot, source hashes and binary hash. ZIP SHA-256 is written alongside it. It does not bundle ripgrep or private workbooks.

The manifest says `source_state=working-tree` and records the base commit plus file digests. This is not a claim that the tree has been committed/tagged.

## Separate public-release gates (not performed locally)

- [ ] Native Windows/Linux/macOS CI on the release revision.
- [ ] User-authorized commit/tag/push and public release upload.
- [ ] Verify go install from a published version in a clean environment.

No remote CI status, public availability or clean Git state should be inferred from this local package.
