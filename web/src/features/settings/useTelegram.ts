// Fetch orchestration for TelegramPanel: the binding, and the two-phase link
// (start, poll, confirm) docs/adr/0010-binding-a-chat-needs-a-confirm.md
// describes.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchAndParse } from "../../api/client";
import { telegramPollInterval } from "./copy";
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
  return fetchAndParse(telegramBindingSchema, "/api/v1/auth/telegram");
}

export function useTelegramBinding() {
  return useQuery({ queryKey: telegramBindingQueryKey, queryFn: fetchTelegramBinding });
}

async function fetchTelegramLinkStatus(linkId: string): Promise<TelegramLinkStatus> {
  return fetchAndParse(
    telegramLinkStatusSchema,
    `/api/v1/auth/telegram/link/${encodeURIComponent(linkId)}`,
  );
}

export function useTelegramLinkStatus(linkId: string | null) {
  return useQuery({
    queryKey: telegramLinkStatusQueryKey(linkId ?? "none"),
    queryFn: () => fetchTelegramLinkStatus(linkId as string),
    enabled: linkId !== null,
    refetchInterval: (query) => telegramPollInterval(query.state.data?.status),
  });
}

export function useStartTelegramLink() {
  return useMutation({
    mutationFn: async (): Promise<TelegramLinkStart> => {
      return fetchAndParse(telegramLinkStartSchema, "/api/v1/auth/telegram/link", { method: "POST" });
    },
  });
}

export function useConfirmTelegramLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (linkId: string): Promise<TelegramBinding> => {
      return fetchAndParse(
        telegramBindingSchema,
        `/api/v1/auth/telegram/link/${encodeURIComponent(linkId)}/confirm`,
        { method: "POST" },
      );
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: telegramBindingQueryKey }),
  });
}

export function useDisconnectTelegram() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<TelegramBinding> => {
      return fetchAndParse(telegramBindingSchema, "/api/v1/auth/telegram", { method: "DELETE" });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: telegramBindingQueryKey }),
  });
}
