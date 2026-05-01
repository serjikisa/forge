# Backlog

## Open

- [ ] **LLM eval suite** — Add a dataset of prompts and expected agent behaviors (e.g. "read main.go" should call `read_file` with path `main.go`). Score how often each model picks the right tool with the right arguments.
- [ ] **Homebrew distribution** — Create a Homebrew tap (forge-cli/tap) with a formula that builds from source or downloads a pre-built binary. Set up GitHub Actions to publish releases with goreleaser.
- [ ] **Versioning and update check** — Embed version at build time via ldflags. Add `forge update` or a startup check that compares local version against latest GitHub release.
- [ ] **Context window awareness** — Query model context size from provider and use it instead of hardcoded 6000 token budget for history compaction.
- [ ] **Retry with backoff** — Add retry logic for transient API errors (429, 503) in cloud providers.
- [ ] **Streaming for `forge ask`** — Stream tokens to stdout in single-shot mode instead of buffering until complete.
