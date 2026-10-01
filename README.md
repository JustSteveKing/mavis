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
the log of calls and notes, follow-ups, `today`), time tracking, stats, and
quotes, and invoices from draft to PDF to paid, with credit notes to
correct them and Peppol e-invoices beside them. Time and invoicing are both optional: if you only want
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

That creates `clients/`, `engagements/`, `log/`, `time/`, `invoices/` and `quotes/` in `~/business` and
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

Change anything else with `client set`, which touches only the fields you
give and keeps whatever you added to the note by hand:

```bash
mavis client set acme --address "1 High Street" --address "Manchester M1 1AA" --country GB
mavis client set acme --terms 14 --phone ""     # an empty value removes a field
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

## Quotes

```bash
mavis quote new globex --title "Reporting rebuild" \
  --scope "A rebuilt reporting module, with CSV and PDF exports." \
  --line "Discovery and design=3 x 650 day" --line "Build=10 x 650 day"
mavis quote send globex
mavis quote accept Q-2026-001 --engagement reporting
```

A quote has a title, a scope and lines. The scope is prose, what the client
is getting, and it is printed above the lines; write it with `--scope` or in
the draft with `mavis quote edit`. Lines and VAT work as they do on invoices.

`send` numbers the quote (`Q-2026-001`), freezes it, and writes its PDF
beside your invoices. It does not email anything. The client's address is
not needed, since a prospect may not have given you one. A quote stands for
30 days unless `--valid` said otherwise, and one left unanswered after that
shows as expired; nothing has to be rewritten for that to happen.

`accept` records the answer, and with `--engagement` starts the work: an
active engagement titled after the quote and linked to it. It is fixed price
with the quote's net total as its budget, unless you give `--basis day --rate
650`, in which case its time invoices like any other day-rate work. If the
engagement cannot be made, because the name is taken say, the quote stays
open so you can try again. `decline` records a no.

Quotes waiting on an answer are listed in `today`, oldest first, and `stats`
counts quotes sent, accepted, declined and still waiting.

## Invoices

Invoices start as drafts:

```console
$ mavis invoice new acme --month 2026-10
Drafted draft-acme-2026-10

  DESCRIPTION                      QTY  UNIT   PRICE      VAT        AMOUNT
  Bug fixes, October 2026         2.75  hour   90.00      20%        247.50
  Reporting module, October 2026     5  day   650.00      20%      3,250.00
                                                          Net      3,497.50
                                                      VAT 20%        699.50
                                                        Total  4,197.00 GBP
```

`--month` adds one line per engagement that has something to bill that
month: day and hourly work from its timesheet at its rate, and a retainer's
monthly rate if it was running. Fixed-price work is never billed from time;
add the milestone by hand:

```bash
mavis invoice new initech --line "Rebuild: design milestone=4000"
mavis invoice new acme --month 2026-10 --line "Workshop=2 x 500 day"
```

A month is never billed twice. An engagement that another invoice already
covers for that month is left off and named, and logging time into a month
that is already invoiced draws a warning, since that time is not on it.

A draft is a table in `invoices/`, so change it in Obsidian or with
`mavis invoice edit`. Amount is always worked out again from Qty and Price,
so editing a quantity cannot leave a total wrong, and a line mavis cannot
read is an error with its line number, never a line quietly dropped.
`mavis invoice discard` deletes a draft.

VAT follows the client. A client in the UK, or with no country set, is
charged 20%. Anywhere else is reverse charged at 0%. `client set
--vat-treatment` overrides either way.

### Retainers

Retainers are billed in arrears, one invoice per month: November's is due
from 1 December, and `today` says so.

```bash
mavis invoice retainers           # what is due
mavis invoice retainers --draft   # draft each one
```

Each draft holds the retainer and nothing else, named
`draft-<engagement>-<month>`, ready to check and issue. Any day or hourly
work for the same client goes on its own invoice as before. Every finished
month since the retainer started stays listed until something bills it, so
a month you forgot comes back instead of slipping by. A retainer with no
start date offers only its most recent month. Paused retainers are never
due, and a done one stops at its end date.

### Issuing

```bash
mavis invoice issue acme-2026-10
mavis invoice paid INV-2026-001 --date 2026-11-20
```

`issue` gives a draft the next number for its year (`INV-2026-001`, then
`INV-2026-002`, and from January `INV-2027-001`), stamps the issue date, tax
point and due date from the client's terms, and renames the file to its
number. Numbers come from the invoices that exist, so there is no counter to
drift out of step with them.

It checks everything a VAT invoice has to show before it numbers anything:
your name, address and VAT number from the config, and the client's address.
Whatever is missing is listed together, with how to fill each one in.

Your details and the client's are copied into the issued invoice. Move house
or let a client rename themselves, and last year's invoices still say what
they said. After that an invoice does not change, except to be marked paid;
`invoice unpaid` takes back a paid mark made by mistake. If an issued
invoice's lines are edited by hand, mavis notices that they no longer add up
to the totals it was issued with and refuses to read it, because the
correction for an issued invoice is a credit note.

### Credit notes

```bash
mavis invoice credit INV-2026-001 --full
mavis invoice credit INV-2026-001 --line "Disputed day=1 x 650 day"
mavis invoice issue draft-credit-inv-2026-001
```

A credit note is drafted against an issued invoice, either every line of it
or the part you give, and issued like an invoice in its own series,
`CN-2026-001`. It can never take more than is left on the invoice; that is
checked when it is drafted and again when it is issued, so two drafts that
each fit cannot add up to too much.

An invoice's balance is its total less its issued credit notes, and that
balance is what `today`, `invoice list` and the unpaid totals in `stats`
use. A fully credited invoice shows as `credited` and stops covering its
month, so you can bill that month again correctly. A part-credited one still
covers it.

Issuing also writes the PDF, to `.invoices/INV-2026-001.pdf` under your
records directory. The folder is dot-prefixed so Obsidian leaves the
binaries out of its index. `mavis invoice pdf` regenerates one, writes it
elsewhere with `--out`, or previews a draft, which is headed DRAFT INVOICE
and carries no number so it cannot pass for the real thing. An issued
invoice's PDF is drawn from the details copied into it, so regenerating one
from last year gives you last year's invoice.

The PDF uses the standard PDF fonts, which cover £, € and accented Latin
letters but not other scripts.

### E-invoices

mavis can write each issued invoice and credit note as a Peppol e-invoice:
BIS Billing 3.0, in UBL 2.1, saved beside its PDF as
`.invoices/INV-2026-001.xml`. UK B2B e-invoicing is not mandatory yet, so
this is opt-in. Setting your own Peppol ID is the switch:

```yaml
business:
  peppol_id: 9932:GB123456789   # 9932 is the scheme for a UK VAT number
```

Then each client needs theirs, and a buyer reference, which Peppol requires
on every invoice and most clients will give you as a purchase order number:

```bash
mavis client set acme --peppol-id 9932:GB123456789 --buyer-reference PO-4471
```

From then on `issue` writes the e-invoice whenever it can, and when it
cannot, lists what is missing with the Peppol rule each gap breaks. A
document Peppol would reject is never written. `mavis invoice ubl` writes
one on demand, or explains why it cannot.

Peppol IDs are never worked out from a VAT number, because
`9932:GB123456789` and `9932:123456789` are different participants on the
network and only one of them may be registered.

A UK address that ends in a postcode has it read out as the postcode, and
the town before it as the city. Reverse-charged invoices carry category
`AE` with your reverse charge wording as the reason. mavis writes the
e-invoice; it does not send it. Sending goes through a Peppol access point.

### Chasing

```bash
mavis invoice remind INV-2026-001          # draft it
mavis invoice remind INV-2026-001 --sent   # once you have sent it
```

`remind` prints a subject and message for an overdue invoice, and the PDF
to attach. It does not send anything. The wording follows the chase so far:
a friendly first nudge, a second that mentions the first, and a final one
asking for payment within 7 days. Each asks for what is still owed after
any credit notes.

`--sent` logs it against the client as an email, with the message kept in
the entry, so the next reminder knows where things stand and `today` can
say when another is due, 14 days after the last. Backdate one with
`--date`, and the message is written as of that day.

The final reminder can cite the Late Payment of Commercial Debts (Interest)
Act 1998 and the fixed compensation it allows a UK business (£40, £70 or
£100, depending on the debt). That is off unless you set
`invoicing.statutory_notice: true`, because it changes the tone with a
client and that is your call.

Overdue invoices head `today`, and `stats` gains an invoices section once
anything has been issued: issued and paid in the period, and what is unpaid
now.

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
invoices/draft-acme-2026-10.md
quotes/Q-2026-001.md
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
business:                     # you, as invoices name you
  name: Your Name Ltd
  address: [1 Your Street, Your Town, AB1 2CD]
  vat_number: GB123456789
  email: you@example.com
  country: GB                 # where you are, for e-invoices
  peppol_id: 9932:GB123456789 # opts in to e-invoices
invoicing:
  reverse_charge_note: 'Reverse charge: the customer is to account for any VAT due.'
  statutory_notice: false     # cite the Late Payment Act in final reminders
thresholds:
  active_quiet: 14
  warm_keep_in_touch: 30
  warm_to_cold: 60
```

Everything but `root` is optional, and nothing under `business` is needed
until you issue an invoice. The reverse charge note above is a placeholder:
check it against how your accountant words it before you rely on it. `--root` or `MAVIS_ROOT` override `root` for
one command, and every command takes `--json`.

## Licence

MIT.
