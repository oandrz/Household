// The Telegram panel. Two phases, deliberately: the deep link only tells the
// bot which account is asking, and the binding is written when this panel --
// inside the member's own session -- confirms the chat that turned up. A
// leaked deep link therefore connects nobody. See
// docs/adr/0010-binding-a-chat-needs-a-confirm.md before simplifying this
// into a single click.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { formatTelegramLinkedAt, telegramChatLabel, telegramPollInterval } from "./copy";
import {
  telegramBindingSchema,
  telegramLinkStartSchema,
  telegramLinkStatusSchema,
  type TelegramBinding,
  type TelegramLinkStart,
  type TelegramLinkStatus,
} from "./schemas";

const telegramBindingQueryKey = ["telegram-binding"] as const;

function telegramLinkStatusQueryKey(linkId: string) {
  return ["telegram-link-status", linkId] as const;
}

async function fetchTelegramBinding(): Promise<TelegramBinding> {
  const body = await apiFetch<unknown>("/api/v1/auth/telegram");
  return telegramBindingSchema.parse(body);
}

function useTelegramBinding() {
  return useQuery({ queryKey: telegramBindingQueryKey, queryFn: fetchTelegramBinding });
}

async function fetchTelegramLinkStatus(linkId: string): Promise<TelegramLinkStatus> {
  const body = await apiFetch<unknown>(
    `/api/v1/auth/telegram/link/${encodeURIComponent(linkId)}`,
  );
  return telegramLinkStatusSchema.parse(body);
}

function useTelegramLinkStatus(linkId: string | null) {
  return useQuery({
    queryKey: telegramLinkStatusQueryKey(linkId ?? "none"),
    queryFn: () => fetchTelegramLinkStatus(linkId as string),
    enabled: linkId !== null,
    refetchInterval: (query) => telegramPollInterval(query.state.data?.status),
  });
}

function useStartTelegramLink() {
  return useMutation({
    mutationFn: async (): Promise<TelegramLinkStart> => {
      const body = await apiFetch<unknown>("/api/v1/auth/telegram/link", { method: "POST" });
      return telegramLinkStartSchema.parse(body);
    },
  });
}

function useConfirmTelegramLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (linkId: string): Promise<TelegramBinding> => {
      const body = await apiFetch<unknown>(
        `/api/v1/auth/telegram/link/${encodeURIComponent(linkId)}/confirm`,
        { method: "POST" },
      );
      return telegramBindingSchema.parse(body);
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: telegramBindingQueryKey }),
  });
}

function useDisconnectTelegram() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<TelegramBinding> => {
      const body = await apiFetch<unknown>("/api/v1/auth/telegram", { method: "DELETE" });
      return telegramBindingSchema.parse(body);
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: telegramBindingQueryKey }),
  });
}

export function TelegramPanel() {
  const binding = useTelegramBinding();
  // Held in component state, not the URL or storage: a reload legitimately
  // abandons an in-progress attempt (the nonce expires on its own inside ten
  // minutes regardless), so there is nothing here worth persisting past it.
  const [linkId, setLinkId] = useState<string | null>(null);
  const linkStatus = useTelegramLinkStatus(linkId);
  const startLink = useStartTelegramLink();
  const confirmLink = useConfirmTelegramLink();
  const disconnect = useDisconnectTelegram();

  // A 404 here means this install has no Telegram bot configured
  // (handleTelegramBinding's own comment: it answers exactly like an
  // unrouted path, on purpose) -- render nothing rather than a broken card
  // an install without a bot would otherwise be stuck with. Nothing else in
  // this panel treats 404 specially; every other query or mutation failure
  // is a genuine error and gets the usual inline message.
  const bindingUnavailable = binding.isError && binding.error instanceof ApiError && binding.error.status === 404;
  if (bindingUnavailable) {
    return null;
  }

  function handleConnect() {
    startLink.mutate(undefined, {
      // The brief resolves on the simple shape here, not SignInScreen's
      // pre-opened-blank-tab trick: `window.open` runs in this awaited
      // mutation's onSuccess, after the click's own user-activation gesture
      // has already passed, which is exactly the pattern SignInScreen's own
      // comment says WebKit's popup gate reliably blocks. Accepted for this
      // panel specifically because there is a fallback: the waiting view
      // below also renders the URL as a plain link, so a blocked popup
      // leaves a click still available rather than a dead end.
      onSuccess: (start) => {
        window.open(start.url, "_blank", "noopener");
        setLinkId(start.id);
      },
    });
  }

  function handleConfirm() {
    if (linkId === null) return;
    confirmLink.mutate(linkId, { onSuccess: () => setLinkId(null) });
  }

  function handleStartOver() {
    setLinkId(null);
  }

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 className="mb-4 text-sm font-semibold text-ink">Telegram</h2>

      {binding.isPending && <p className="text-xs text-muted">Loading…</p>}

      {binding.isError && !bindingUnavailable && (
        <p role="alert" className="text-xs text-danger">
          Couldn't load your Telegram connection.
        </p>
      )}

      {binding.isSuccess && binding.data.connected && (
        <div className="flex flex-col gap-3 text-[13px]">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-ink">
                Connected as{" "}
                <span className="font-semibold">
                  {telegramChatLabel(binding.data.chatUsername)}
                </span>
              </div>
              {binding.data.linkedAt && (
                <div className="mt-0.5 text-[11.5px] text-muted">
                  Linked {formatTelegramLinkedAt(binding.data.linkedAt)}
                </div>
              )}
            </div>
            <button
              type="button"
              onClick={() => disconnect.mutate()}
              disabled={disconnect.isPending}
              className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
            >
              Disconnect
            </button>
          </div>
          {disconnect.isError && (
            <p role="alert" className="text-[11px] text-danger">
              {apiErrorMessage(disconnect.error, "Something went wrong disconnecting that. Please try again.")}
            </p>
          )}
        </div>
      )}

      {binding.isSuccess && !binding.data.connected && linkId !== null && (
        <div className="flex flex-col gap-3 text-[13px]">
          {(linkStatus.data?.status === "waiting" || linkStatus.isPending) && (
            <p className="text-muted">
              Open Telegram and press Start to connect this chat. We'll check automatically.
              {startLink.data && (
                <>
                  {" "}
                  Didn't open?{" "}
                  <a
                    href={startLink.data.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-semibold text-accent"
                  >
                    Open Telegram
                  </a>
                  .
                </>
              )}
            </p>
          )}

          {linkStatus.data?.status === "pending" && (
            <>
              <p className="text-ink">
                <span className="font-semibold">
                  {telegramChatLabel(linkStatus.data.chatUsername)}
                </span>{" "}
                opened this link. Confirm it's you to finish connecting.
              </p>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={handleConfirm}
                  disabled={confirmLink.isPending}
                  className="min-h-11 rounded-lg bg-accent px-3 py-1.5 text-[11px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
                >
                  Confirm
                </button>
              </div>
              {confirmLink.isError && (
                <p role="alert" className="text-[11px] text-danger">
                  {apiErrorMessage(confirmLink.error, "Something went wrong confirming that. Please try again.")}
                </p>
              )}
            </>
          )}

          {linkStatus.data?.status === "refused" && (
            <>
              <p role="alert" className="text-danger">
                {linkStatus.data.reason ?? "That link couldn't be used. Try again."}
              </p>
              <div>
                <button
                  type="button"
                  onClick={handleStartOver}
                  className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
                >
                  Start over
                </button>
              </div>
            </>
          )}

          {linkStatus.data?.status === "expired" && (
            <>
              <p className="text-muted">That link expired. Start again to get a new one.</p>
              <div>
                <button
                  type="button"
                  onClick={handleStartOver}
                  className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
                >
                  Start over
                </button>
              </div>
            </>
          )}

          {linkStatus.data?.status === "connected" && (
            <p className="text-muted">Connected.</p>
          )}

          {linkStatus.isError && (
            <>
              <p role="alert" className="text-[11px] text-danger">
                Couldn't check that link's status. Please try again.
              </p>
              <div>
                <button
                  type="button"
                  onClick={handleStartOver}
                  className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label sm:min-h-0"
                >
                  Start over
                </button>
              </div>
            </>
          )}
        </div>
      )}

      {binding.isSuccess && !binding.data.connected && linkId === null && (
        <div className="flex flex-col gap-2 text-[13px]">
          <p className="text-muted">
            Connect a Telegram chat to get reminders and use the bot from there.
          </p>
          <div>
            <button
              type="button"
              onClick={handleConnect}
              disabled={startLink.isPending}
              className="min-h-11 rounded-lg border border-hairline px-3 py-1.5 text-[11px] font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
            >
              Connect Telegram
            </button>
          </div>
          {startLink.isError && (
            <p role="alert" className="text-[11px] text-danger">
              {apiErrorMessage(startLink.error, "Something went wrong starting that. Please try again.")}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
