// Copy, QR and Share-to-Telegram for one invite link -- pulled out of
// PendingInviteCard.tsx so that file stays about *state* (which of the four
// cards is showing) and this one stays about *one link*. A QR canvas in the
// middle of a state machine is the file nobody wants to read.
//
// The QR is drawn in the browser by qrcode-generator, never fetched from a
// server: the link is a live credential for the next 24 hours (or until the
// household withdraws it), and an image-endpoint approach would put that
// credential in a server access log and a shared browser/CDN cache. Kept
// entirely client-side, the link never leaves this tab except by the
// owner's own Copy/Share action.
import { useMemo, useState } from "react";
import qrcode from "qrcode-generator";

export function InviteLinkShare({ link }: { link: string }) {
  const [copied, setCopied] = useState(false);

  // qrcode-generator's own API: typeNumber 0 lets it pick the smallest size
  // that fits the data instead of this component guessing one, and "M"
  // (recovers ~15% of the code) is the middling error-correction level --
  // plenty for a code shown on a screen for the twenty seconds it takes to
  // scan it, and denser (and so harder to scan) than the codebase needs.
  const svg = useMemo(() => {
    const qr = qrcode(0, "M");
    qr.addData(link);
    qr.make();
    return qr.createSvgTag({ cellSize: 4, margin: 2 });
  }, [link]);

  async function handleCopy() {
    // navigator.clipboard is undefined in a non-secure context (plain HTTP)
    // and in jsdom unless a test stubs it -- guard rather than assume it
    // exists, the same guard AdminMailPage.tsx's LinkRow uses for the same
    // reason: an operator (or here, an owner) on an unusual setup still sees
    // the link text instead of a thrown error.
    if (!navigator.clipboard) return;
    await navigator.clipboard.writeText(link);
    setCopied(true);
  }

  function handleShare() {
    // t.me's own share intent -- no API call, no server round trip, just a
    // URL Telegram's client recognises.
    const shareUrl = `https://t.me/share/url?url=${encodeURIComponent(link)}`;
    window.open(shareUrl, "_blank", "noopener");
  }

  return (
    <div className="flex flex-col gap-3">
      {/* qrcode-generator returns markup built from this component's own `qr`
          object (never from a string that could hold untrusted input -- the
          link is always this household's own), so setting it directly is
          safe here the way it would not be for arbitrary HTML. */}
      <div
        role="img"
        aria-label="QR code for the invite link"
        className="mx-auto h-[148px] w-[148px] [&_svg]:h-full [&_svg]:w-full"
        dangerouslySetInnerHTML={{ __html: svg }}
      />
      <div className="flex items-center gap-2 rounded-lg border border-hairline bg-canvas px-3 py-2">
        <span className="min-w-0 flex-1 break-all text-[12px] text-ink">{link}</span>
      </div>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => void handleCopy()}
          className="min-h-11 flex-1 rounded-lg border border-hairline px-3 py-1.5 text-[12px] font-semibold text-label sm:min-h-0"
        >
          {copied ? "Copied" : "Copy"}
        </button>
        <button
          type="button"
          onClick={handleShare}
          className="min-h-11 flex-1 rounded-lg border border-hairline px-3 py-1.5 text-[12px] font-semibold text-label sm:min-h-0"
        >
          Share to Telegram
        </button>
      </div>
    </div>
  );
}
