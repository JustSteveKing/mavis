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
make check                                  # exactly what CI runs
make sandbox                                # seeded throwaway records to try things in
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

**Timesheets are tables, parsed row by row.** One file per engagement per
month (`time/<engagement>-<YYYY-MM>.md`), because that is the unit that gets
invoiced and it keeps the engagement's backlinks. A row that will not parse
becomes a `Problem` with its line number; it never fails the listing.
`appendRow` inserts after the last table row so prose below the table stays
below it. Pipes in descriptions are escaped as `\|` and `cells` unescapes them.

**Durations are minutes against a configurable day.** `internal/duration`
converts `1d`, `3h`, `1h30m` to minutes using `Store.DayMinutes`, set from
`day_hours`. The sheet keeps what was typed (`1d`), so changing the day length
later re-values old entries; that is intended, since a day rate is a rate per
day, whatever a day is.

**Invoices are notes with a lines table** (`internal/store/invoice.go`).
Amount is never read back from the file; it is Qty times Price every time,
so a hand edit cannot leave a stale total. Unlike a timesheet, a line that
will not parse makes the whole invoice a `Problem`: dropping a line from an
invoice is worse than refusing to read it. VAT is totalled per rate,
rounding once per rate rather than per line.

**Issuing freezes by snapshot.** `IssueInvoice` copies the issuer
(`from_*`) and the client (`to_*`) into the invoice, plus the reverse charge
wording as `vat_note`, so rendering an old invoice never reads today's
config or client note. Use the snapshot fields when rendering an issued
invoice, never the client record. It also stores `net`, `vat` and `total`;
`checkFrozen` recomputes from the lines and refuses a file whose lines no
longer match, which is how a hand edit after issue is caught.

**PDFs come from `internal/pdf`, rendered with maroto v2.** Chosen
because it is maintained and lays out in rows and columns; go-pdf/fpdf and
jung-kurt/gofpdf are archived. It costs binary size: stripped, mavis went
from 4.4 MB to 18.3 MB, nearly all of it maroto's dependencies. Rendering
reads only the invoice's snapshot fields. Text goes through the core fonts'
Windows-1252, so characters outside it are lost; embedding a TTF would fix
that. Tests render with `Options{Uncompressed: true}` and search the bytes,
so they need no PDF tooling in CI. The `cmd` layer writes the PDF after
`IssueInvoice` returns, because the store cannot import `pdf` (which imports
the store); a failed PDF leaves the invoice issued and says how to retry.

**Credit notes are invoices with `kind: credit`** and `credits: [[INV-...]]`.
They share the file format, the lines table and `IssueInvoice`; `prefix`
gives them their own `CN` series. `settle` derives each invoice's
`Credited` and `Balance` from issued credit notes every time invoices are
listed; neither is stored. Anything asking "what is owed" must use
`Balance`, never `Total`. Over-crediting is checked at draft and again at
issue, because two drafts can each fit and together not. A fully credited
invoice (`FullyCredited`) no longer covers its month for `linesForMonth`
and `InvoiceCovering`, which is what lets the month be re-billed. A credit
note has no due date and cannot be paid.

**Quotes mirror invoices** (`internal/store/quote.go`): the same lines
table, `parseLines`, `totals`, `checkFrozen` and snapshot fields, their own
`Q` series, and a `scope` that is simply the body above the table. Expired
is derived from `valid_until`, like an invoice's balance. `DecideQuote`
makes the engagement before it marks the quote, so a failed engagement
leaves the quote open; it lets go of the lock in between and re-checks the
status after.

**`internal/pdf` renders a `Doc`, not an invoice.** `FromInvoice` and
`FromQuote` map onto it, so the layout exists once. Units mavis writes (day,
hour, month) are pluralised on the page; units typed by hand are not.

**`internal/ubl` writes Peppol BIS 3.0 UBL**, mirroring einvoicing.dev's
`UblWriter` (api.einvoicing.dev) so the same invoice comes out the same
from either: same element order, same categories (S, Z, AE with
`VATEX-EU-AE`), same unit codes. `Check` runs first and reports gaps under
their Peppol or EN 16931 rule ids; `Write` refuses a document `Check` finds
wanting. The tests validate every document against the official UBL 2.1
XSDs, vendored in `internal/ubl/testdata/xsd`, with `xmllint`. CI installs
it and sets `MAVIS_REQUIRE_XMLLINT` so a missing validator fails the build
instead of skipping. The Peppol Schematron rules are not run here; they
need the KoSIT validator. E-invoicing is opt-in: `issue` only attempts UBL
once `business.peppol_id` is set, so freelancers not on Peppol never see it.
Peppol IDs are never derived from VAT numbers.

**Retainer months due are derived** (`RetainersDue`): every finished month
from the engagement's start to its end or last month, less any month an
invoice covers (`period` plus `engagements`, ignoring fully credited ones).
Billed in arrears, so the current month is never due. Without a start date
only the latest finished month is offered, never a guessed backlog.
`DraftRetainer` is `AddInvoice` restricted to one engagement
(`NewInvoice.Engagement`), so the same double-billing guard applies.

**Reminders are log entries** with `invoice` and `reminder: N` in the
frontmatter and the message under `## As sent`. The stage is counted from
them (`store.Reminders`), never stored on the invoice. `internal/remind`
drafts and never sends. A reminder written with `--date` is composed as of
that date, counting only earlier reminders: the log is a record of what was
said, so it must be true on its day. The Late Payment Act paragraph is
opt-in (`invoicing.statutory_notice`), and its compensation bands (£40
under £1,000, £70 under £10,000, £100 above) were checked against GOV.UK.

**`internal/mcpserver` is the agent surface, and its boundary is the
point.** Agents get reads, record keeping and drafts; issuing, sending,
paying, answering quotes, recording reminders as sent and discarding drafts
are deliberately absent, and `TestTheLineIsHeld` fails if a tool name
containing issue, send, paid, accept, decline, discard or delete appears.
Adding one is a product decision, not a refactor. Tools take exact slugs and
numbers (the `client`, `engagement`, `invoice` and `quote` helpers reject
anything the fuzzy resolver would have had to guess). `mavis mcp` sets
`Store.Actor` to `agent`, which stamps `by: agent` on created records.
Money fields in JSON are integer pence and named `*_pence`, for agents and
`--json` alike; making them strings would hit the schema-inference trap
taskgo found (a custom MarshalJSON on a numeric type). The stdio runner
returns nil on a client disconnect, as taskgo's does, for the same reason.
`instructions.go` is what every connecting agent is told; keep it short.

**TUI forms stay open on error** (`form.go`): the store's message is shown
in the form and the input kept. Rows are `cells`, aligned across the view
by `columnWidths`/`alignCells` with a cap, and dates go through `rel`.

**`internal/tui` follows taskgo's TUI**: lazygit-style panels, the 16
ANSI colours, a reload every two seconds that never fires while typing or
confirming (`TestAgentChangesAppearOnTheTickButNotWhileTyping`). It acts as
the human, so it may mark invoices paid, always behind a y/n. The cursor is
shown by a `›` marker as well as colour, so it survives a monochrome
terminal. `TestFrameFitsTheTerminal` renders at three sizes and fails on any
line wider than the terminal or a frame of the wrong height; the footer is
truncated rather than wrapped for that reason.

**`internal/termquiet` exists because Bubble Tea v1 queries the terminal's
background colour in its package `init`**, so every mavis command, not just
the TUI, would wait out a five-second timeout on a terminal that never
answers. termquiet declares the background first, relying on Go's specified
package initialisation order (imports first, then import path, and
`JustSteveKing` sorts before `charmbracelet`). `main.go` imports it for that
alone. `TestNoBackgroundQuery` in package main runs the built binary in a
pty (creack/pty, test-only) and fails if the query goes out or startup takes
seconds; removing the import makes it fail at 4s. Measured: 5.03s to 0.02s.

**`init` refuses a code project**, a folder with a go.mod, package.json or
similar, or one inside such a project's git work tree, before creating
anything; records there would be committed with the code. A plain git repo
with no manifest (a vault) is allowed, so moving into brain stays possible.
It happened for real: `mavis init` run inside this repo made it the root.

**Folders come from `store.Layout`, never constants.** `s.layout.Clients`
and its siblings, `s.FilesDir()` for PDFs and XML. The constants were
removed so nothing can quietly use the old names; `pdf.Write` takes the
files folder rather than the root. `Validate` refuses absolute paths,
escapes, the root itself, and two kinds of record sharing or nesting in one
folder, since records are told apart by folder as well as by `type`.
`Stray` reports records left in a default folder the layout no longer
points at; `openStore` prints it as a warning on every command. mavis never
moves files to follow a layout change.

**Invoice numbers are derived** from existing files (`nextNumber`), per year
of the issue date, under the store lock. There is no counter. A gap would
need a deleted issued invoice, and issued invoices cannot be discarded.

**`IssueInvoice` reports every missing detail at once** (`MissingError`),
each with the command or config key that fixes it. Keep it that way: one
gap per attempt makes issuing a guessing game.

**Whether a month is billed is derived, not stamped.** An invoice records
`period` and `engagements`; `linesForMonth` skips an engagement any invoice
already covers for that month, and `InvoiceCovering` drives the warning in
`mavis time`. Nothing is written to timesheets. Discarding a draft frees the
month again, which is the point.

**Bad table rows report file line numbers**, via `record.Document.LineOf`,
which counts the frontmatter. Body-relative numbers were wrong in an editor.

**Map iteration order is random; frontmatter key order must not be.** When
a function sets several keys that may be new, iterate a slice. `SetClient`
once used a map and would have reshuffled new keys on every run.

**Money is never a float.** Files hold decimal strings (`"650.00"`);
`internal/money` parses them to integer pence and refuses more than two
decimal places rather than rounding. Multiply and divide with
`Pence.MulDiv`, which rounds half away from zero in integers.

**Stats are value, never revenue.** `Store.Stats` values time at the
engagement's rate; revenue needs invoices and must stay absent until they
exist, not appear as a guess. Rules worth not relitigating:

- Fixed-price work has no per-period value. A fee does not belong to a month.
  It is reported to date in `Fixed`, as budget over days logged.
- A retainer is worth its rate for every month of the period it ran,
  logged time or not (`retainerMonths`).
- A total's `PerDay` counts only rows with both value and time. An earlier
  version counted a retainer with no time logged and reported a flattering
  1,677.27 a day where the logged work earned 654.55.
- Totals are per currency. Never add amounts across currencies.

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
