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

## Files

```
api/internal/domain/amount_parse.go             ParseAmount, FormatAmount, MinorUnitsFor
api/internal/usecase/telegram_command.go        TelegramCallerService (resolve), TelegramCommandService (LogSpend, Balances, Recent)
api/internal/adapter/telegram/commands.go       ParseCommand, Commander (the guard and the reply)
api/internal/adapter/telegram/poller.go         WithCommands, dispatchCommand
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
- Real bot: **not walked** — see ADR 8's last consequence. 🟡 until a
  development bot token exists for the dev stack.
