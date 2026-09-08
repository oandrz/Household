# Telegram chat commands — design

**Date:** 2026-09-08. **Status:** built in the same change, on branch
`hearthctl-agent`. Stage 5a of the automation roadmap: the household's chat
as a way to log spending, beside the browser and `hearthctl`.

## Decisions

1. **A fixed grammar, no free text.** `/spend <amount> <what> [#category]
   [@account]`, `/income …`, `/balance`, `/recent`, `/help`. Quoted names
   allow spaces (`#"Dining out"`). Anything else is ignored, never guessed.
   Free text through a language model is stage 5b, behind a key, with a
   confirmation step; this stage needs none because every command is explicit.
2. **The guard is the adapter's** (ADR 8): chat → membership through a
   resolver that decides nothing, then owner and money capability, before
   any service is called.
3. **Amounts are typed in the account's currency** and parsed with integer
   string arithmetic (`domain.ParseAmount`), places taken from the currency
   table (`SGD` 2, `JPY` 0, `BHD` 3). More decimals than the currency has
   is refused, not rounded. No `float64`.
4. **Account defaults to the household's only cash account.** Two or more,
   or none, is refused with the list; `@name` picks one, case-insensitively.
   Categories match the transaction's kind only, so an expense is never
   offered an income category.
5. **The transaction is dated today (UTC)**, paid by the member who sent
   the message.
6. **Idempotency key is the update id.** A redelivered update replays.
7. **Replies never carry internal error text.** A refusal the person can
   act on is explained (which account, which category, how to write the
   amount); anything else is logged and answered generically.
8. **Off unless a bot is configured**, like sign-in; on the same poller,
   enabled with `WithCommands`.

## Stage 5b — free text, behind a key, written only on /yes

9. **`usecase.IntentParser` is a port**; `adapter/anthropic` is its one
   implementation, built only when `ANTHROPIC_API_KEY` is set. Without it
   the bot answers a sentence with "commands only, /help". Nothing else in
   the product depends on the key.
10. **The model is `claude-opus-5` at effort `low`**, one strict tool
    (`log_transaction`: kind, amount, description, account, category),
    `tool_choice: auto` with `disable_parallel_tool_use`, the household's
    account and category names in the system prompt so the model echoes a
    real name. No tool call, a refusal, or a kind the schema did not name
    all read as "none". Errors name the operation, never the URL.
11. **A parse is never a write.** The Commander shows the reading back —
    "Log expense 84.50 — groceries #Groceries @DBS Savings? Reply /yes or
    /no." — and holds it per chat for five minutes. `/yes` writes it with
    the **sentence's** update id as the idempotency key; `/no` discards;
    a second `/yes`, or one after expiry, writes nothing and says so.
12. **The guard runs before the parser.** A stranger's or a limited
    member's sentence never reaches the API, so it cannot cost money or
    leak names.
13. **A sentence from a chat that may not write is ignored silently.**
    Before free text, a non-slash message was ignored; keeping that for
    strangers and limited members means the change is invisible to them,
    and no stranger can farm outbound sends (the same Telegram budget
    sign-in links use). A slash command from the same chat still gets its
    one-sentence refusal. **No per-chat limit on parser calls** for an
    authorised owner yet — a named gap, since no key exists anywhere today.
14. **Tested against a fake Messages API** that checks the request shape
    (model, strict closed schema, tool choice, effort, no `thinking` sent)
    and the parse of the reply. The first real call, once the owner added a
    key, was refused for lack of credits (`Your credit balance is too low`),
    which led to the next decision.
15. **A second parser, through OpenRouter, so the feature can be free.**
    The owner asked for an open-source model. The port already existed, so
    this is `adapter/openrouter`: OpenRouter's OpenAI-dialect
    `chat/completions` over plain `net/http` (no SDK, no new dependency),
    one **forced** tool call — with `auto`, small models answer in prose and
    every sentence would read as "none" — `max_tokens` 4096 for reasoning
    variants, nothing dialect-specific beyond that (`strict`,
    `parallel_tool_calls` and `reasoning` are not sent, because OpenRouter
    forwards unknown parameters and some upstream providers refuse them).
    Tool arguments arrive as a JSON string per the dialect but as an object
    from some hosts; both are read. What the two adapters share — prompt,
    schema, and the fail-closed reader of arguments — moved to
    `adapter/intent`, so a model that invents a kind is refused identically
    whichever spoke. Configuration: `OPENROUTER_API_KEY` + `OPENROUTER_MODEL`,
    both or neither, never beside `ANTHROPIC_API_KEY` (refused at boot: no
    silent precedence). **The model id is configuration, not code**, because
    OpenRouter's free, tool-capable list changes month to month. Errors carry
    the status and the provider's message only — the Anthropic log line that
    prompted this carried a request id and a workspace id, which a log does
    not need. The Commander now caps every parser call at 30 s, whichever
    adapter: the poller handles one update at a time, and a stalled free-tier
    queue would otherwise hold every chat. Quality is the accepted trade: an
    open-weight model follows the schema less reliably than Claude, which is
    what the confirm-on-`/yes` step exists for.

## Files

```
api/internal/domain/amount_parse.go             ParseAmount, FormatAmount, MinorUnitsFor
api/internal/usecase/telegram_command.go        TelegramCallerService (resolve), TelegramCommandService (LogSpend, Balances, Recent)
api/internal/adapter/telegram/commands.go       ParseCommand, Commander (the guard and the reply)
api/internal/adapter/telegram/poller.go         WithCommands, dispatchCommand
api/internal/usecase/intent.go                  IntentParser port, Intent
api/internal/adapter/anthropic/intent_parser.go the Claude adapter (stage 5b)
api/cmd/api/main.go                             wiring
```

## Testing

- Domain: parse per currency, refuse over-precision, sign, garbage; format
  round-trips.
- Usecase: sole-cash default, ambiguity refused, named account resolves,
  category kind filter, JPY refuses `12.5`, income goes *to* the account,
  the update id is the key and a second call replays.
- Adapter: grammar table; unlinked chat refused before any service call;
  limited member and owner-without-money refused; owner spend reaches the
  service with the update id and gets a receipt; replay says "Already
  logged"; refusals explained, internal errors not leaked; usage on a
  missing amount; poller dispatches only with `WithCommands`.
- Real bot: **walked on 2026-09-08** against `@HearthOinkDevBot` from the
  owner's linked chat (`/balance`, `/spend 4.50 coffee`, `/recent`; ledger
  row keyed `telegram-update-172668165`). A first attempt with the shared
  production token failed exactly as ADR 8 describes.
