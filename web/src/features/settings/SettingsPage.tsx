// The Settings screen (design/Household Dashboard.dc.html's is_settings
// panel). Four of the design's five cards: Members, Spaces, Currency &
// region, and Notifications. The fifth, Connected accounts, belongs to a
// later slice (per the task brief) and is omitted entirely -- not rendered
// disabled, not stubbed, simply not here yet.
import { PageContainer } from "../../components/PageContainer";
import { CurrencyPanel } from "./CurrencyPanel";
import { MembersPanel } from "./MembersPanel";
import { NotificationsPanel } from "./NotificationsPanel";
import { SpacesPanel } from "./SpacesPanel";

export function SettingsPage() {
  return (
    <PageContainer>
      <div>
        <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
          Settings
        </h1>
        {/* The design's own subtitle names "connections" -- the Connected
            accounts panel this refers to isn't built this slice (see the
            header comment above). Kept the literal design string rather
            than editing it to match what's actually on screen today; it
            describes where this screen is headed, not just its current
            state. */}
        <p className="mt-1 text-[13px] text-muted">
          Members, spaces, currency, connections &amp; privacy
        </p>
      </div>

      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
        <MembersPanel />
        <SpacesPanel />
        <CurrencyPanel />
      </div>

      <NotificationsPanel />
    </PageContainer>
  );
}
