// The operator's outbound mail. Two components, two routes: the list, and one
// message. Opening a message is a separate request on purpose -- it is its
// own audit row, which is what makes seeing a live link a deliberate act with
// a record rather than the default state of a screen.
//
// Nothing here renders a message's HTML. The links are extracted server-side
// (domain.ExtractLinks) and arrive as strings; the body arrives as plain
// text. See the spec's decision 1 for what was rejected and why.
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { PageContainer } from "../../components/PageContainer";
import { ApiError } from "../../api/client";
import type { AdminMailList, AdminMailMessage, AdminMailSummary } from "./adminOutboxSchemas";
import { exactTimeLabel } from "./directoryCopy";
import {
  OUTBOX_DEFAULT_LIMIT,
  useAdminMail,
  useAdminMailMessage,
} from "./useAdminOutbox";
import { isNotFound, useCloseSurfaceOnReauth } from "./useAdminDirectory";
import { isAdminLayerFailure } from "./useAdmin";

// The two unavailable states get different copy because they have different
// fixes: one is an unset variable, the other is a container that is down.
// Anything else falls through to the generic message the error carries.
function outboxErrorCopy(error: unknown): string | null {
  if (!(error instanceof ApiError)) return null;
  if (error.code === "MAIL_INSPECTOR_NOT_CONFIGURED") {
    return "The message inspector is not configured on this install. Set MAILPIT_API_URL and restart the API.";
  }
  if (error.code === "MAIL_UPSTREAM_UNAVAILABLE") {
    return "Mailpit is not answering. The messages are not lost — the reader is.";
  }
  return error.message;
}

export function AdminMailPage() {
  const query = useAdminMail(OUTBOX_DEFAULT_LIMIT);
  useCloseSurfaceOnReauth(query.error);

  // A gate-layer failure (lapsed grant, admin revoked) is about to be
  // replaced by AdminShell's own gate; rendering it inline too would flash
  // a second message for the same failure.
  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">
        Outbound mail
      </h1>
      {/* Decision 9: the production Mailpit service has no volume, so this
          is the honest boundary of the screen rather than a defect to
          report. */}
      <p className="mt-1 text-[13px] text-muted">
        Mailpit keeps these only until it restarts — a deploy or a reboot
        clears them. Send a fresh link rather than looking for an old one.
      </p>

      {inlineError && (
        <div
          role="alert"
          className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          {outboxErrorCopy(inlineError) ??
            "Something went wrong loading the outbox."}
        </div>
      )}

      {query.isPending ? (
        <MailSkeleton />
      ) : query.data ? (
        <MailTable data={query.data} />
      ) : null}
    </PageContainer>
  );
}

function MailSkeleton() {
  return (
    <div
      data-testid="mail-skeleton"
      aria-hidden="true"
      className="flex flex-col divide-y divide-hairline"
    >
      {[0, 1, 2, 3, 4].map((i) => (
        <div key={i} className="h-10 bg-canvas" />
      ))}
    </div>
  );
}

function MailTable({ data }: { data: AdminMailList }) {
  if (data.messages.length === 0) {
    return <p className="text-[13px] text-muted">No mail sent yet.</p>;
  }
  return (
    <div className="flex flex-col gap-3">
      <table className="w-full text-[12.5px]">
        <thead className="hidden md:table-header-group">
          <tr className="text-left text-[11.5px] font-semibold text-label">
            <th scope="col" className="py-1.5 pr-3">
              To
            </th>
            <th scope="col" className="py-1.5 pr-3">
              Subject
            </th>
            <th scope="col" className="py-1.5">
              Sent
            </th>
          </tr>
        </thead>
        <tbody>
          {data.messages.map((message) => (
            <MailRow key={message.id} message={message} />
          ))}
        </tbody>
      </table>
      <p className="text-[12px] text-muted">
        {data.truncated
          ? `Showing the newest ${data.messages.length} of ${data.total}`
          : `Showing ${data.messages.length} of ${data.total}`}
      </p>
    </div>
  );
}

// One row per message: recipient, subject, time. No body text, ever --
// AdminMailSummary (adminOutboxSchemas.ts) has no field to render one even by
// mistake, since the list's own .strict() schema would refuse a server
// response that tried to add one.
function MailRow({ message }: { message: AdminMailSummary }) {
  return (
    <tr className="border-b border-hairline last:border-b-0 md:table-row">
      <td className="block py-2 pr-3 md:table-cell">
        <Link
          to="/admin/mail/$messageId"
          params={{ messageId: message.id }}
          className="font-semibold text-ink hover:text-accent"
        >
          {message.to}
        </Link>
        <span className="text-muted md:hidden"> · {message.subject}</span>
      </td>
      <td className="hidden py-2 pr-3 md:table-cell">{message.subject}</td>
      <td className="block pb-2 text-muted md:table-cell md:py-2">
        {exactTimeLabel(message.sentAt)}
      </td>
    </tr>
  );
}

// The message view follows AdminHouseholdPage.tsx, NOT the list page above,
// and the difference is the whole point: isAdminLayerFailure treats
// NOT_FOUND as "the admin surface is gone, let AdminGate handle it", which is
// right on a list and wrong here. On this route a 404 means Mailpit no longer
// holds the message -- ordinary, expected (its store has no volume), and the
// screen has its own copy for it.
//
// So the miss is tested against query.error itself, never against the
// gate-filtered inlineError below. That is the invariant, and it is not the
// same claim as "the check comes first": inlineError is a const, so moving
// the two lines past each other changes nothing. What would break the screen
// is reading isNotFound(inlineError) -- the filter has already dropped every
// NOT_FOUND, so that test is false every single time and this branch would
// never run. AdminDatabasePage.tsx's row viewer carries the same invariant
// for the same reason.
export function AdminMailMessagePage({ messageId }: { messageId: string }) {
  const query = useAdminMailMessage(messageId);
  useCloseSurfaceOnReauth(query.error);

  if (isNotFound(query.error)) {
    return (
      <PageContainer>
        <Link
          to="/admin/mail"
          className="text-[12.5px] font-medium text-muted hover:text-ink"
        >
          ‹ Outbound mail
        </Link>
        <p className="mt-4 text-[13px]">
          Mailpit no longer holds this message. Its store is cleared whenever
          the container restarts — send a fresh link rather than looking for
          this one.
        </p>
      </PageContainer>
    );
  }

  const inlineError =
    query.error && !isAdminLayerFailure(query.error) ? query.error : null;

  return (
    <PageContainer>
      <Link
        to="/admin/mail"
        className="text-[12.5px] font-medium text-muted hover:text-ink"
      >
        ‹ Outbound mail
      </Link>

      {inlineError && (
        <div
          role="alert"
          className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] text-danger"
        >
          {outboxErrorCopy(inlineError) ??
            "Something went wrong loading this message."}
        </div>
      )}

      {query.isPending ? (
        <div
          data-testid="mail-message-skeleton"
          aria-hidden="true"
          className="flex flex-col gap-3"
        >
          <div className="h-7 w-48 rounded bg-canvas" />
          <div className="h-4 w-80 rounded bg-canvas" />
          <div className="mt-4 h-24 rounded bg-canvas" />
        </div>
      ) : query.data ? (
        <MessageDetail data={query.data} />
      ) : null}
    </PageContainer>
  );
}

function MessageDetail({ data }: { data: AdminMailMessage }) {
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="font-serif text-[22px] font-medium tracking-[-0.01em]">
          {data.subject}
        </h1>
        <p className="mt-0.5 text-[12.5px] text-muted">
          To {data.to} · sent {exactTimeLabel(data.sentAt)}
        </p>
      </header>

      <section
        aria-labelledby="mail-links-heading"
        className="flex flex-col gap-2"
      >
        <h2
          id="mail-links-heading"
          className="text-[13px] font-semibold text-ink"
        >
          Links
        </h2>
        {data.links.length === 0 ? (
          <p className="text-[12.5px] text-muted">
            No links were found in this message.
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {data.links.map((link) => (
              <LinkRow key={link} link={link} />
            ))}
          </ul>
        )}
      </section>

      <section
        aria-labelledby="mail-text-heading"
        className="flex flex-col gap-2"
      >
        <h2
          id="mail-text-heading"
          className="text-[13px] font-semibold text-ink"
        >
          Message text
        </h2>
        {/* Plain text, never HTML -- see this file's header comment and the
            spec's decision 1. whitespace-pre-wrap keeps the sender's own
            line breaks without needing to render markup to do it. */}
        <p className="whitespace-pre-wrap rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[12.5px] text-ink">
          {data.text}
        </p>
      </section>
    </div>
  );
}

function LinkRow({ link }: { link: string }) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    // navigator.clipboard is undefined in a non-secure context (plain HTTP)
    // and in jsdom unless a test stubs it -- guard rather than assume it
    // exists, so an operator on an unusual setup still sees the link text
    // instead of a thrown error.
    if (!navigator.clipboard) return;
    await navigator.clipboard.writeText(link);
    setCopied(true);
  }

  return (
    <li className="flex items-center gap-2 rounded-lg border border-hairline bg-card px-3.5 py-2.5">
      <span className="min-w-0 flex-1 break-all text-[12.5px] text-ink">
        {link}
      </span>
      <button
        type="button"
        onClick={() => void handleCopy()}
        className="shrink-0 rounded-lg bg-accent px-2.5 py-1 text-[12px] font-semibold text-white active:translate-y-px"
      >
        {copied ? "Copied" : "Copy link"}
      </button>
    </li>
  );
}
