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

mavis is early. What exists today is the client side (clients, engagements,
the log of calls and notes, follow-ups, `today`), time tracking and stats.
Invoicing comes next. Time and invoicing are both optional: if you only want
somewhere to keep track of clients, you never have to meet either.

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

That creates `clients/`, `engagements/`, `log/` and `time/` in `~/business` and
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

## Time

Log time after the fact, in days or hours, whichever suits the work:

```bash
mavis time reporting 1d "Report filters"
mavis time reporting 2h "Call with Jo" --date yesterday
mavis time list                          # this month
mavis time list --month 2026-09 --client acme
```

Durations are `1d`, `0.5d`, `3h`, `45m` or `1h30m`. A day is 7.5 hours unless
you set `day_hours` in the config, and listings total in both.

Each engagement gets one timesheet a month, `time/acme-reporting-2026-10.md`,
holding a table:

```markdown
| Date | Time | What |
|------|------|------|
| 2026-10-06 | 1d | Report filters |
| 2026-10-07 | 2h | Call with Jo |
```

Fix a mistake by editing the table, in Obsidian or with
`mavis time edit reporting`. A row mavis cannot read is reported with its line
number and left out of the totals, rather than hiding the rest of the sheet.
New rows go at the end of the table, so a note you write under it stays put.

## Stats

```console
$ mavis stats --month 2026-10
October 2026

CLIENT   ENGAGEMENT        BASIS                         TIME     VALUE   PER DAY
acme     Bug fixes         hourly @ 90.00       0.37d (2h45m)    247.50    675.00
acme     Reporting module  day @ 650.00           5d (37h30m)  3,250.00    650.00
globex   Support           retainer @ 1,500.00      0.4d (3h)  1,500.00  3,750.00
initech  Rebuild           fixed                2.5d (18h45m)         -         -
Total                                             8.27d (62h)  4,997.50    866.62

Fixed price, to date
  initech  Rebuild  budget 12,000.00  10.5d logged  1,142.86 a day
```

`stats` shows the time you logged in a period and what it is worth at your
rates. The default is this month; `--month`, `--year`, or `--from` and `--to`
pick another, so a tax year is `--from 2026-04-06 --to 2027-04-05`.

Value depends on the engagement's basis. Day and hourly work is time times
the rate. A retainer counts its monthly rate for each month of the period it
was running, whether or not you logged time against it.

Fixed-price work gets no value for a period, because the fee does not belong
to any one month. Give the engagement a `--budget` and it appears under
**Fixed price, to date**: the budget over every day logged so far, which is
the number that tells you whether a fixed bid is still paying.

PER DAY on the total line counts only engagements with both a value and time
in the period. Unpriced time does not drag it down, and a retainer with
nothing logged does not push it up. Clients billed in different currencies
get a total each; they are never added together.

This is value, not revenue. What you actually invoiced and were paid is a
question for invoices, which are not built yet.

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
time/acme-reporting-2026-10.md
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
day_hours: 7.5
thresholds:
  active_quiet: 14
  warm_keep_in_touch: 30
  warm_to_cold: 60
```

Everything but `root` is optional. `--root` or `MAVIS_ROOT` override `root` for
one command, and every command takes `--json`.

## Licence

MIT.
