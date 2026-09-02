// One household, read-only: who is in it, how they sign in, whether its
// password sign-in is currently locked, and who has been invited but has
// not arrived. No money -- the spec's boundary; financial data costs the
// database browse's deliberate second step.
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import { NotFoundScreen } from "../shell/NotFoundScreen";
import { memberBadgeLabel } from "../settings/copy";
import type {
  AdminHouseholdMember,
  AdminHouseholdPage as PageData,
} from "./adminDirectorySchemas";
import {
  dateLabel,
  exactTimeLabel,
  lockoutLabel,
  relativeTimeLabel,
} from "./directoryCopy";
import {
  isNotFound,
  useAdminHousehold,
  useCloseSurfaceOnReauth,
} from "./useAdminDirectory";
import { isAdminLayerFailure } from "./useAdmin";

export function AdminHouseholdPage({ householdId }: { householdId: string }) {
  const query = useAdminHousehold(householdId);
  useCloseSurfaceOnReauth(query.error);

  // A 404 here is "no such household" (or a malformed id, which the API
  // answers identically) and renders the same screen a non-admin sees for
  // the whole subtree -- nothing about the miss is worth distinguishing.
  if (isNotFound(query.error)) return <NotFoundScreen />;

  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      {/* search is required here, not optional: adminHouseholdsRoute's own
          validateSearch always returns a concrete {q, limit}, so a caller
          landing back on that route needs an explicit starting point -- a
          blank search, the same "no filter" state a fresh visit gets. */}
      <Link
        to="/admin/households"
        search={{ q: "", limit: 50 }}
        className="text-[12.5px] font-medium text-muted hover:text-ink"
      >
        ‹ Households
      </Link>

      {inlineError && (
        <div
          role="alert"
          className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          {inlineError instanceof ApiError
            ? inlineError.message
            : "Something went wrong loading the household."}
        </div>
      )}

      {query.isPending ? (
        <div
          data-testid="household-skeleton"
          aria-hidden="true"
          className="flex flex-col gap-3"
        >
          <div className="h-7 w-48 rounded bg-canvas" />
          <div className="h-4 w-80 rounded bg-canvas" />
          <div className="mt-4 h-24 rounded bg-canvas" />
        </div>
      ) : query.data ? (
        <HouseholdDetail data={query.data} />
      ) : null}
    </PageContainer>
  );
}

function HouseholdDetail({ data }: { data: PageData }) {
  const now = new Date();
  const { household, members, pendingInvites, lockout } = data;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">
          {household.name}
        </h1>
        <p className="mt-0.5 text-[12.5px] text-muted">
          Family {household.familyName} · created{" "}
          {dateLabel(household.createdAt)} · {household.primaryCurrency} ·{" "}
          {members.length} {members.length === 1 ? "member" : "members"}
        </p>
      </header>

      {lockout && (
        <div
          role="status"
          className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          <p className="font-semibold">
            {lockoutLabel(lockout.lockedUntil, now)}
          </p>
          <p className="mt-0.5">
            Clear it early with{" "}
            <code className="font-semibold">
              adminctl unlock-household --email &lt;owner&gt;
            </code>
            .
          </p>
        </div>
      )}

      <section
        aria-labelledby="members-heading"
        className="flex flex-col gap-2"
      >
        <h2 id="members-heading" className="text-[13px] font-semibold text-ink">
          Members
        </h2>
        <table aria-label="Members" className="w-full text-[12.5px]">
          <thead className="hidden md:table-header-group">
            <tr className="text-left text-[11.5px] font-semibold text-label">
              <th scope="col" className="py-1.5 pr-3">
                Name
              </th>
              <th scope="col" className="py-1.5 pr-3">
                Channel
              </th>
              <th scope="col" className="py-1.5 pr-3">
                Role
              </th>
              <th scope="col" className="py-1.5 pr-3">
                Capabilities
              </th>
              <th scope="col" className="py-1.5">
                Last active
              </th>
            </tr>
          </thead>
          <tbody>
            {members.map((m) => (
              <MemberRow key={m.userId} member={m} now={now} />
            ))}
          </tbody>
        </table>
      </section>

      <section
        aria-labelledby="invites-heading"
        className="flex flex-col gap-2"
      >
        <h2 id="invites-heading" className="text-[13px] font-semibold text-ink">
          Pending invites
        </h2>
        {pendingInvites.length === 0 ? (
          <p className="text-[12.5px] text-muted">None pending.</p>
        ) : (
          <ul className="divide-y divide-hairline text-[12.5px]">
            {pendingInvites.map((invite) => (
              <li
                key={invite.email}
                className="flex flex-col gap-0.5 py-2 md:flex-row md:items-center md:gap-4"
              >
                <span className="font-semibold text-ink">{invite.name}</span>
                <span className="text-ink">{invite.email}</span>
                <span className="text-muted">
                  {memberBadgeLabel(invite.role)}
                </span>
                <span className="text-muted">
                  invited by {invite.invitedByName}
                </span>
                <span className="text-muted">
                  expires {dateLabel(invite.expiresAt)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

// Channel is text: the email address itself, or the word "Telegram". No
// icon -- the operator is reading a support ticket, not scanning a feed.
function MemberRow({
  member,
  now,
}: {
  member: AdminHouseholdMember;
  now: Date;
}) {
  const channel =
    member.channel === "telegram" ? "Telegram" : (member.email ?? "");
  const lastActive = relativeTimeLabel(member.lastActiveAt, now);
  const badge =
    member.role === "limited"
      ? "bg-badge-limited-bg text-label"
      : "bg-badge-owner-bg text-accent";
  return (
    <tr className="border-b border-hairline last:border-b-0">
      <td className="block py-2 pr-3 md:table-cell">
        <span className="font-semibold text-ink">{member.name}</span>
        <span className="text-muted md:hidden"> · {channel}</span>
      </td>
      <td className="hidden py-2 pr-3 md:table-cell">{channel}</td>
      <td className="block pb-2 pr-3 md:table-cell md:py-2">
        <span
          className={`rounded-full px-2 py-0.5 text-[11px] font-semibold ${badge}`}
        >
          {memberBadgeLabel(member.role)}
        </span>
        <span className="ml-2 text-muted md:hidden">
          {member.capabilities.join(" ")} ·{" "}
        </span>
        <span
          className="text-muted md:hidden"
          title={
            member.lastActiveAt
              ? exactTimeLabel(member.lastActiveAt)
              : undefined
          }
        >
          {lastActive}
        </span>
      </td>
      <td className="hidden py-2 pr-3 text-muted md:table-cell">
        {member.capabilities.join(" ")}
      </td>
      <td
        className={`hidden py-2 md:table-cell ${member.lastActiveAt ? "" : "text-muted"}`}
        title={
          member.lastActiveAt ? exactTimeLabel(member.lastActiveAt) : undefined
        }
      >
        {lastActive}
      </td>
    </tr>
  );
}
