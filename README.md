# mavis

Run a freelance business from plain files.

mavis keeps your clients, the work you do for them, every call and meeting,
and what you promised to do next, as Markdown files in a folder. Point it at
an Obsidian vault and every record is a note you can open, link to and edit
there too. Nothing lives in a database, so there is nothing to export and
nothing to lose: `cat` a client, `grep` your calls, `git diff` a rate change.

```console
$ mavis today
Overdue
  Send estimate  acme · due 2026-09-28 (3 days ago)

Due this week
  Share staging access  acme · due 2026-10-04

Active engagements
  acme  Reporting module  day · since 2026-10-06

Worth a move?
  hooli    active, but no active engagement and quiet for 122 days → warm?
  initech  warm, quiet for 78 days → cold?

Keep in touch
  globex  warm, quiet for 40 days · last contact 2026-08-22
```

Named for the 1920s private secretary, who took your calls, kept your diary
and knew exactly who still owed you.

mavis is early. What exists today is the client side: clients, engagements,
the log of calls and notes, follow-ups, and `today`. Time tracking and
invoicing come next, and both stay optional. If you only want somewhere to
keep track of clients, you never have to meet either.

## Install

From a release, download the archive for your platform from the releases
page, unpack it and put `mavis` on your PATH. Linux and macOS, on amd64 and
arm64.

From source, with Go 1.27 or later:

```bash
git clone https://github.com/JustSteveKing/mavis.git
cd mavis && go build -ldflags "-X main.version=$(git describe --tags --always)" -o mavis .
```

## Start

```bash
mavis init ~/business
```

That creates `clients/`, `engagements/` and `log/` in `~/business` and
remembers it as your records directory. Run it inside an Obsidian vault and
the records become notes in it.

```bash
mavis client add acme --name "Acme Ltd" --contact "Jo Bloggs" --email jo@acme.test
mavis engagement add acme reporting --title "Reporting module" --basis day --rate 650
mavis log call acme "Scoped the reporting module" -f "Send estimate" --due +7d
mavis today
```

The slug you give a client (`acme`) is its file name and how links reach it,
so keep it short. Everywhere else, mavis takes any unique part of a name:
`mavis client show acm` finds Acme, and if two clients match it lists both
rather than picking one.

## Clients

A client has a temperature, and you set it:

| Status | Means |
|---|---|
| `prospect` | not a client yet |
| `active` | working together now |
| `warm` | no work on, but the relationship is alive and more is likely |
| `cold` | gone quiet; more work would take effort to win |

```bash
mavis client warm acme
mavis client list --status warm
mavis client show acme
```

Moving a client stamps `status_since`, so you can see how long Acme has been
cold. `client show` lists their engagements, open follow-ups and most recent
log entries.

mavis will suggest a move in `today`, and it will never make one. Whether a
client is warm is your judgement, and a tool that quietly reclassified people
would be wrong in exactly the cases you care about.

## Engagements

An engagement is one piece of work for one client. A client can have several
at once: a fixed-price build and a monthly retainer, say.

```bash
mavis engagement add acme reporting --basis day --rate 650
mavis engagement list --client acme
mavis engagement done reporting
```

Statuses are `proposed`, `active`, `paused` and `done`. Moving to `active`
stamps a start date if there is none, and `done` stamps the end. Basis
(`day`, `hourly`, `fixed`, `retainer`) and rate are optional, and only matter
once you track time or invoice.

## The log

Calls, meetings, emails and notes all go in one log, one file each, so a
client has a single timeline.

```bash
mavis log call acme "Scoped the reporting module" --with "Jo Bloggs" -e reporting
mavis log meeting acme                # no summary: opens $EDITOR
mavis note acme "Moving to Postgres next quarter"
```

Leave out the summary and mavis opens the new entry in `$VISUAL` or
`$EDITOR`. It only does that at a terminal, so a script or an agent calling
mavis never hangs on an editor nobody is there to close. `--date` backdates
an entry.

### Follow-ups

A follow-up is a checkbox in a log entry:

```bash
mavis log call acme "Scoped it" -f "Send estimate" --due +7d -f "Book the next call"
mavis follow-ups --overdue
mavis done acme estimate
```

`--due` takes a date or `+3d` / `+2w`. Give one `--due` for all the
follow-ups, or one each in order.

Because they are plain checkboxes, you can tick them in Obsidian just as well
as with `mavis done`, and add new ones by hand anywhere in an entry. The one
piece of syntax mavis reads is a trailing `(due YYYY-MM-DD)`.

## today

`today` is the one to run each morning. It shows overdue follow-ups and those
due in the next seven days, your active engagements, and clients worth a
look:

- an **active** client with no active engagement, quiet for 14 days: move to warm?
- a **warm** client quiet for 30 days: get in touch
- a **warm** client quiet for 60 days: move to cold?

Quiet counts from the last log entry or the last time you moved the client,
whichever is later. Moving a client is a fresh decision, so it resets the
clock. Cold clients never appear; `mavis client list --status cold` is there
for when you mean to work through them.

## The files

```
clients/acme.md
engagements/acme-reporting.md
log/2026-10-01-acme-call.md
```

A log entry looks like this:

```markdown
---
type: log
kind: call
date: 2026-10-01T14:30
client: '[[acme]]'
engagement: '[[acme-reporting]]'
with: [Jo Bloggs]
---
Scoped the reporting module. They want exports by end of month.

## Follow-ups
- [x] Send estimate (due 2026-10-08)
- [ ] Share staging access
```

Records point at each other with `[[wikilinks]]`, so in Obsidian a client's
backlinks are its engagements and its whole log. Frontmatter stays flat (text,
lists and dates) because that is what Obsidian's Properties panel can edit and
what Bases can build a table from.

**Edit anything by hand.** When mavis rewrites a file, it keeps what you
added: extra properties, their order, comments, and the body exactly as you
left it. A reference you type as `acme`, `[[acme|Acme Ltd]]` or
`[[clients/acme]]` all mean the same client.

Two guards keep links working. `client add acme` refuses if a note called
`acme.md` already exists anywhere in the folder, since Obsidian would no
longer know which one `[[acme]]` means. Folders starting with a dot are
skipped, as Obsidian skips them.

## Configuration

`~/.config/mavis/config.yaml`. `mavis init` writes `root`; the thresholds
are yours to add, and these are the defaults:

```yaml
root: /home/you/business
thresholds:
  active_quiet: 14
  warm_keep_in_touch: 30
  warm_to_cold: 60
```

Every threshold is optional. `--root` or `MAVIS_ROOT` override `root` for
one command, and every command takes `--json`.

## Licence

MIT.
