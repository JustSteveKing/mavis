# mavis CLI

Every command takes `--json` for machine-readable output and `--root` to
point at a records folder other than the configured one. Names given to the
CLI may be any unique part of a slug or name; it lists the candidates rather
than guessing when one is ambiguous.

Commands marked **user only** are irreversible or reach a client. Do not run
them unless the user tells you to.

## Daily

| Command | Does |
|---|---|
| `mavis today` | What needs attention now |
| `mavis log <call\|meeting\|email\|note> <client> "<summary>" [-e engagement] [-f "follow-up" --due +3d]` | Log something that happened |
| `mavis note <client> "<text>"` | Shorthand for `log note` |
| `mavis follow-ups [--overdue] [--client c]` | Open follow-ups |
| `mavis done [client] <words>` | Tick off a follow-up |
| `mavis time <engagement> <duration> "<what>" [--date YYYY-MM-DD]` | Log time |
| `mavis time list [--month YYYY-MM] [--client c]` | Time for a month |
| `mavis stats [--month YYYY-MM \| --year YYYY \| --from D --to D]` | Time, value, invoices, quotes |
| `mavis tui` | The interactive view |
| `mavis web [--port n] [--no-open]` | Read-only view in the browser; runs until stopped, so start it only when the user asks |

## Clients and engagements

| Command | Does |
|---|---|
| `mavis client add <slug> --name "..." [--contact --email --phone --status]` | Add a client |
| `mavis client list [--status s]` / `client show <client>` | Read |
| `mavis client set <client> [--address line --country GB --vat-number ... --peppol-id ... --buyer-reference ... --terms 30]` | Change details |
| `mavis client <prospect\|active\|warm\|cold> <client>` | Move a client, only when asked |
| `mavis engagement add <client> <name> --title "..." [--basis day --rate 650 --budget ... --start ...]` | Add an engagement |
| `mavis engagement list [--client c]` / `engagement show <e>` | Read |
| `mavis engagement <proposed\|active\|paused\|done> <e>` | Move an engagement |

## Billing

| Command | Does |
|---|---|
| `mavis invoice new <client> [--month YYYY-MM] [--line "desc=qty x price unit"]` | Draft an invoice |
| `mavis invoice retainers [--draft]` | Retainer months due, and draft them |
| `mavis invoice list` / `invoice show <ref>` | Read |
| `mavis invoice credit <INV> --full \| --line "..."` | Draft a credit note |
| `mavis invoice remind <INV>` | Draft a payment reminder (prints it) |
| `mavis invoice issue <draft>` | **User only.** Number and freeze, write PDF |
| `mavis invoice paid <INV>` / `invoice unpaid <INV>` | **User only.** |
| `mavis invoice remind <INV> --sent` | **User only.** Records a reminder as sent |
| `mavis invoice discard <draft>` | **User only.** Deletes a draft |
| `mavis invoice pdf <ref>` / `invoice ubl <INV>` | Write the PDF or Peppol e-invoice |
| `mavis quote new <client> --title "..." [--scope "..."] [--line "..."]` | Draft a quote |
| `mavis quote list` / `quote show <ref>` | Read |
| `mavis quote send <draft>` | **User only.** Number, freeze, write PDF |
| `mavis quote accept <Q> [--engagement name]` / `quote decline <Q>` | **User only.** |
| `mavis quote discard <draft>` | **User only.** |

## Setup

| Command | Does |
|---|---|
| `mavis init <dir>` | Create a records folder; asks first inside an existing git repository |
| `mavis init <dir> --from <git-url>` | Set up another machine from a records repository |
| `mavis mcp` | The MCP server, on stdio, started by the agent's client |
| `mavis completion install [bash\|zsh\|fish]` | Install tab completion where the shell loads it |
