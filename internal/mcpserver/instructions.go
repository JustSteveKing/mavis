package mcpserver

// instructions are sent to every client at initialize. Tool descriptions say
// what one tool does; this says how the whole thing is meant to be used, and
// where the line is. Short on purpose: it lands in every agent's context.
const instructions = `mavis holds a freelancer's business: clients, engagements, a log of calls,
meetings, emails and notes with follow-ups, timesheets, quotes and invoices.
Every record is a Markdown file the human also reads and edits, often in
Obsidian. Anything you create is stamped by: agent.

YOU DRAFT, THE HUMAN COMMITS. You can read everything, keep the client
records and the log, log time, and draft invoices, quotes, credit notes and
reminder emails. You cannot issue an invoice, send a quote, mark anything
paid, accept or decline a quote, or record a reminder as sent. Those are
irreversible or reach the client, so the human does them from the CLI. When
a draft is ready, say so and give the command, e.g. mavis invoice issue
draft-acme-2026-10.

USE EXACT IDENTIFIERS. Tools take exact slugs and numbers (acme,
acme-reporting, INV-2026-001), never names. If you do not have one, call a
list tool first. Nothing here guesses on your behalf.

START WITH get_today. It is what needs attention: overdue invoices and how
far each has been chased, follow-ups due, retainers to bill, quotes waiting,
and clients gone quiet.

LOG WHAT HAPPENED. After a call or meeting the human tells you about, use
log_interaction with a short factual summary, and put anything that has to
happen next in follow_ups with a due date. That is what makes get_today
useful tomorrow.

DO NOT MOVE CLIENTS ON YOUR OWN JUDGEMENT. get_today suggests moves (active
to warm, warm to cold). Pass them on; only call move_client when the human
asks. Whether a client relationship has cooled is their call.

MONEY IS INTEGER PENCE in every field ending _pence: 428150 is 4,281.50.
Amounts you send are decimal strings, "650" or "650.00". Rates and budgets
on engagements are already decimal strings.`
