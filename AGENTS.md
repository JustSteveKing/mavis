# AGENTS.md

Guidance for coding agents (Claude Code and others) working on this
repository.

mavis is a CLI for running a freelance business whose storage is Markdown
files with YAML frontmatter, laid out so the records directory can be an
Obsidian vault.

`README.md` is for people using mavis: commands, file format, and the
reasoning a user benefits from. This file is for agents changing the code:
architecture, invariants and traps. Keep them apart.

## Commands

```bash
go build ./... && go vet ./... && go test -race ./...
gofmt -l .                                  # CI fails on any output

go test ./internal/store/                   # one package
go test -run TestRoundTrip ./internal/record/

# Never point a test run at a real records directory or the real config.
export XDG_CONFIG_HOME=/tmp/mavis/xdg XDG_CACHE_HOME=/tmp/mavis/xdg/cache
go run . init /tmp/mavis/vault
go run . client add acme
```

## Architecture

**Everything goes through `internal/store`.** `cmd/` is flag parsing and
rendering. Business rules in `cmd/` are rules a future MCP server or TUI
cannot reach, so they belong in the store. `Today` lives there for that
reason, even though only one command calls it now.

**`internal/record` owns the file format, and its promise is that a rewrite
loses nothing a human added.** Frontmatter is held as a `yaml.Node`, never
decoded into a struct and re-encoded, so unknown keys, their order and their
comments survive. The body is kept byte for byte.
`TestRoundTripKeepsWhatAHumanAdded` asserts an unmodified file round-trips
exactly; keep it that way. Any change that decodes frontmatter into a struct
and writes the struct back breaks the product, not just a test.

Three setters, and the choice matters:

- `Set` tags the value `!!str`. YAML quotes it only when it would otherwise
  read as another type, so `"650.00"`, `'[[acme]]'` and `"+441610000000"`
  stay text.
- `SetPlain` leaves the tag empty, so dates stay unquoted and Obsidian reads
  them as dates. Use it for dates and plain integers only.
- `SetList` writes flow style, `[a, b]`.

**Frontmatter stays flat**: text, lists of text, dates. Obsidian's
Properties panel shows nested objects as unsupported and Bases cannot filter
on them. Anything structured goes in the body. Follow-ups are body
checkboxes for exactly this reason.

**Records reference each other by wikilink.** The store writes
`'[[slug]]'`; `linkTarget` reads any form a person might type (`acme`,
`[[acme|Acme]]`, `[[clients/acme]]`, `[[acme#Heading]]`). Compare slugs
through `linkTarget`, never raw frontmatter values.

**Slugs must be unique across the whole directory**, not just within a
section, because Obsidian resolves `[[name]]` by file name. `findNote` walks
the root (skipping dot-directories, as Obsidian does) and `Add*` refuses a
collision. New record types that create files must do the same.

**Two locks.** `withLock` takes an in-process mutex and a `flock`. flock is
per process, so it serialises the CLI against another process but lets
goroutines in one process straight through; the mutex covers that.
`TestConcurrentAddsDoNotCollide` guards it, and CI runs `-race` so a data
race the test happens not to lose still fails the build. The lock file lives in the user cache
directory, keyed by a hash of the root, so a records directory under git
never sees it.

**Writes are atomic**: a dot-prefixed temp file in the same directory, then
rename. Obsidian does not index dot-files, so it never shows the temp file.
Reads take no lock.

**Listing tolerates bad files.** `Clients`, `Engagements` and `Logs` return
a `[]Problem` alongside results. One broken hand edit must not hide every
other record. Files without the right `type:` are skipped silently, which is
how a `README.md` can sit in a section folder.

**Resolution never guesses.** `resolve` takes an exact slug, then a unique
substring of slug or name; several matches return `*AmbiguousError` listing
them. `CompleteFollowUp` follows the same rule. Acting on the wrong record is
worse than asking the user to retype.

**Money is never a float.** Files hold decimal strings (`"650.00"`);
`internal/money` parses them to integer pence and refuses more than two
decimal places rather than rounding.

**The clock is `Store.Now`.** Tests replace it. Never call `time.Now()`
inside the store.

## Behaviour that looks like a bug and is not

- **`today` resets a client's quiet clock when its status moves.** Quiet is
  measured from the later of the last log entry and `status_since`. A move
  is a fresh judgement, and nudging about a client moved yesterday would be
  noise.
- **mavis never changes a client's status on its own.** It suggests in
  `today`. Do not add automatic moves.
- **Any checkbox in a log entry is a follow-up**, not only those under the
  `## Follow-ups` heading mavis writes.
- **`log` with no summary opens an editor only when stdin and stdout are
  both terminals** (`golang.org/x/term`). A file-mode check is not enough:
  `/dev/null` is a character device, and treating it as a terminal hangs
  scripts.

## Scope

Invoicing and time tracking are optional by design. Nothing they need (VAT
number, address, a numbering sequence) may become required for the client
side to work, and stats that only invoices can answer are absent rather than
zero when there are none.

Platforms: Linux and macOS are built and released.
