// The Settings screen (design/Household Dashboard.dc.html's is_settings
// panel). Four of the design's five cards: Members, Spaces, Currency &
// region, and Notifications, plus Telegram -- a card the design itself
// doesn't have, added by the account-linking slice. The design's fifth
// card, Connected accounts (bank sync), is a different feature from
// Telegram despite the similar name and still belongs to a later slice (per
// the task brief) -- not rendered disabled, not stubbed, simply not here
// yet.
import { PageContainer } from "../../components/PageContainer";
import { CurrencyPanel } from "./CurrencyPanel";
import { MembersPanel } from "./MembersPanel";
import { NotificationsPanel } from "./NotificationsPanel";
import { SpacesPanel } from "./SpacesPanel";
import { TelegramPanel } from "./TelegramPanel";

// openInvite arrives from settingsRoute's validated ?invite=true and is passed
// straight through to the panel that owns the modal. Defaulted, so every
// existing `render(<SettingsPage />)` keeps compiling.
export function SettingsPage({ openInvite = false }: { openInvite?: boolean }) {
  return (
    <PageContainer>
      <div>
        <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
          Settings
        </h1>
        {/* The design's own subtitle names "connections" -- originally
            written for the still-unbuilt bank-sync Connected accounts panel
            (see the header comment above), it now also covers Telegram,
            which the design never had a subtitle word for. Kept the literal
            design string either way; it describes where this screen is
            headed, not just its current state. */}
        <p className="mt-1 text-[13px] text-muted">
          Members, spaces, currency, connections &amp; privacy
        </p>
      </div>

      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <MembersPanel openInvite={openInvite} />
        <SpacesPanel />
        <CurrencyPanel />
      </div>

      <NotificationsPanel />
      <TelegramPanel />
    </PageContainer>
  );
}
