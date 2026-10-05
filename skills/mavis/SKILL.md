---
name: mavis
description: Keep a freelancer's business records with mavis, a CLI and MCP server whose records are Markdown files. Use when the user mentions a client, a call or meeting they had, something they promised to do, time they worked, billing or invoices, a quote, chasing a payment, or asks what needs their attention today, and mavis is installed or its MCP tools are connected.
---

# mavis

mavis keeps clients, engagements, a log of calls, meetings, emails and notes
with follow-ups, timesheets, quotes and invoices as Markdown files, often in
an Obsidian vault the user also reads and edits. You help keep those records
and prepare the billing. The user commits anything that cannot be taken
back.

## Which way in

Use the MCP tools when they are connected (`get_today`, `log_interaction` and
the rest; the server is `mavis mcp`). Otherwise use the CLI, with `--json`
when you need to read its output. Both write the same files.

## The line you do not cross

You draft. The user commits.

Never issue an invoice, send a quote, mark anything paid or unpaid, accept or
decline a quote, record a reminder as sent, or discard a draft on the user's
behalf. Those are irreversible or reach their client. The MCP server has no
tools for them at all. With the CLI you could run them, and you must not
unless the user tells you to in so many words. When a draft is ready, say so
and give the exact command, such as `mavis invoice issue draft-acme-2026-10`.

## Identifiers

Records are named by exact slugs and numbers: `acme`, `acme-reporting`,
`INV-2026-001`, `draft-acme-2026-10`. MCP tools take nothing else and refuse
a near miss. If you only have a name, call a list tool (`list_clients`,
`list_engagements`, `list_invoices`) and use the identifier it gives back.
Never guess one.

## Workflows

### "What needs doing?"

Call `get_today` (CLI: `mavis today`). Report it in this order, briefly:

1. Overdue invoices, with how far each has been chased (`chase` says how many
   reminders have gone and whether the next is due).
2. Overdue follow-ups.
3. Retainer months to bill.
4. Quotes waiting on an answer, and any that have expired.
5. Follow-ups due this week.
6. Suggested client moves and keep-in-touch nudges. These are suggestions for
   the user. Do not act on them.

### After a call or meeting

When the user tells you about a call, meeting or email, log it with
`log_interaction` (CLI: `mavis log call <client> "<summary>"`).

- `summary` is short and factual, in the user's terms. Do not embellish.
- Anything that has to happen next is a follow-up, with a due date if the
  user gave one. Dates are `YYYY-MM-DD`, or `+3d` / `+2w` from today. Do not
  invent follow-ups the user did not mention.
- Set `engagement` when the call was about one.

Tick off a follow-up with `complete_follow_up`, naming its log entry and its
exact text, both from `list_follow_ups` (CLI: `mavis done <client> <words>`).

### Time and deliveries

Log time with `log_time` (CLI: `mavis time <engagement> <duration> "<what>"`).
Durations are `1d`, `0.5d`, `3h`, `45m` or `1h30m`; a day is the user's
configured working day, 7.5 hours unless set. If the result carries a
warning that the month is already invoiced, tell the user: that time is not
on the invoice.

Work paid per item (an engagement with `basis: item`, an article or a video)
takes deliveries instead: `log_delivery` with the engagement, the number of
items (default 1) and what was delivered (CLI: `mavis delivered <engagement>
"<what>"`). `log_time` refuses nothing on item work, but time alone there is
never billed; log both if the user wants to know what the work pays by the
day. Add item work with `add_engagement`, basis `item`, a rate per item and a
`unit` such as `article`.

### Month-end billing

1. `list_retainers_due`, then `draft_retainer_invoices` for retainer months
   (billed in arrears, one invoice each).
2. For each client with day or hourly work that month,
   `draft_invoice` with `month` (CLI: `mavis invoice new <client> --month YYYY-MM`).
   A month already on an invoice is skipped, never billed twice; report
   anything in `skipped` and why.
3. Fixed-price work is never billed from time. Add the milestone as a line,
   only if the user tells you the amount.
4. Show the user each draft's lines and total, and give them the
   `mavis invoice issue <draft>` command. Do not issue.

### Chasing a payment

From `get_today`'s `chase`, when a reminder is due, call `draft_reminder`
with the invoice number and give the user the subject and message to send.
The wording escalates on its own from reminders already logged. Once the user
says they sent it, tell them to run `mavis invoice remind <number> --sent`;
that records it.

### Quotes

`draft_quote` with a title, a `scope` in plain prose (what the client gets),
and lines. The user sends it with `mavis quote send <draft>`. When the client
accepts, the user runs `mavis quote accept <number> --engagement <name>`,
which starts the engagement.

### New clients and engagements

`add_client` with a short lowercase slug (`acme`, not `acme-ltd-uk`); it is
the file name and how links reach the client. `add_engagement` with the
client's slug, a short `name`, and a basis (`day`, `hourly`, `fixed`,
`retainer`) and rate if the user gave them. Add billing details (address,
country, VAT number, Peppol ID) with `update_client` when the user provides
them. Never derive a Peppol ID from a VAT number: `9932:GB123456789` and
`9932:123456789` are different participants.

## Things that look wrong and are not

- `draft_invoice` refuses a client whose note says `invoicing:` (FreeAgent,
  Upwork): they are invoiced elsewhere. Tell the user so; do not clear the
  field to get a draft through. Change it only when the user says their
  invoicing has moved.

- Money in tool output is integer pence in every field ending `_pence`:
  `428150` is 4,281.50. Amounts you send are decimal strings, `"650"`.
- Stats report value (time at rates), not revenue. Revenue comes from
  invoices.
- Issued invoices never change. A correction is a credit note
  (`draft_credit_note`), which the user then issues.
- Do not move a client between active, warm and cold unless the user asks.
  Whether a relationship has cooled is their judgement.
- Everything you create is stamped `by: agent` in its file. That is
  intended.

The full list of CLI commands is in [cli.md](cli.md).
