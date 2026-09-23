// The Linked chats group. The caller's own chat is TelegramConnection --
// connect, confirm, disconnect, unchanged from the old Telegram card -- so
// it is left out of the read-only rows below, or it would be drawn twice
// (spec decision 10). The rows are the other members' chats, which an
// owner can see and nobody here can disconnect (decision 6).
import { formatTelegramLinkedAt, telegramChatLabel } from "./copy";
import type { AccessChat } from "./schemas";
import { TelegramConnection } from "./TelegramConnection";

export function LinkedChatList({ chats, myUserId }: { chats: AccessChat[]; myUserId: string }) {
  const others = chats.filter((c) => c.memberId !== myUserId);
  return (
    <div className="flex flex-col gap-3">
      <TelegramConnection />
      {others.length > 0 && (
        <ul className="flex flex-col divide-y divide-hairline border-t border-hairline">
          {others.map((c) => (
            <li key={c.memberId} className="py-2.5 text-[13px]">
              <div className="text-ink">
                {/* "" means Telegram sent no username; the label helper
                    already words that case, given undefined. */}
                <span className="font-semibold">{telegramChatLabel(c.chatUsername || undefined)}</span>{" "}
                <span className="text-muted">· {c.memberName}</span>
              </div>
              <div className="mt-0.5 text-[11.5px] text-muted">Linked {formatTelegramLinkedAt(c.linkedAt)}</div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
