// The single entry point for talking to the Go service. Every request goes
// through here so that credentials, the CSRF header and error decoding are
// handled in exactly one place.
import { PRE_AUTH_API_PREFIXES } from "../routes/publicRoutes";

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly details: Record<string, unknown>;

  constructor(
    status: number,
    code: string,
    message: string,
    details: Record<string, unknown> = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

// unauthorizedHandler is called whenever apiFetch sees a 401 from a request
// that isn't itself exempt (see isPreAuthRequest below). It starts as a
// no-op so this module has no hard dependency on TanStack Query or the
// router -- main.tsx wires the real one once, at startup, via
// setUnauthorizedHandler, giving it a QueryClient to clear and a router to
// navigate with. Kept as module-level state (not a parameter threaded
// through every apiFetch call) because apiFetch's callers -- every query and
// mutation hook in this codebase -- would otherwise all need to learn about
// this concern just to pass it through unchanged.
type UnauthorizedHandler = () => void;
let unauthorizedHandler: UnauthorizedHandler | null = null;

// setUnauthorizedHandler installs (or, with null, removes) the callback
// apiFetch invokes on an unexempt 401. Exported so both main.tsx (the real
// wiring: clear the query cache and redirect to /sign-in) and tests (a
// stub, reset to null afterward so one test's handler can't leak into the
// next) can set it.
export function setUnauthorizedHandler(handler: UnauthorizedHandler | null): void {
  unauthorizedHandler = handler;
}

// PRE_AUTH_API_PREFIXES (imported above, from routes/publicRoutes.ts) names
// the request paths a 401 must never react to: every one of them is
// reachable before any session exists, so a 401 from one of
// them means "that specific attempt failed" (a wrong password, an expired
// magic link, a bad invite token), not "the session this tab thought it had
// is gone." Reacting to a 401 there would clear the cache and bounce a
// caller off the exact screen -- sign-in, or the invite acceptance form --
// they were using to establish a session in the first place.
//
// /api/v1/auth/magic-link covers both /magic-link (request) and
// /magic-link/consume (consume) with one prefix -- neither can 401 today
// (see usecase/auth.go: RequestMagicLink never errors this way, and
// ConsumeMagicLink's failures map to 410, not 401), but the exemption is
// cheap and future-proofs both without needing to track that invariant here.
// /api/v1/invites/ covers both the preview and the accept endpoint the same
// way.
//
// This list is deliberately about *request paths*, not about which screen is
// currently mounted -- GET /api/v1/auth/me is never in it, on purpose, since
// that is the exact endpoint whose 401 unauthorizedHandler exists to react
// to. A screen that is itself reachable pre-auth but still calls useMe()
// (the sign-in screen checking "is someone already signed in"; the invite
// screen checking "are you signed in as someone else") is a different
// problem -- see unauthorizedRedirect.ts's isOnPublicRoute, which the
// installed handler itself is responsible for consulting. Do not try to fix
// that here by adding "/api/v1/auth/me" to this list: that would silence the
// 401 this handler exists for everywhere, not just on those two screens.
function isPreAuthRequest(path: string): boolean {
  return PRE_AUTH_API_PREFIXES.some((prefix) => path.startsWith(prefix));
}

// Not added to PRE_AUTH_API_PREFIXES above: that list is about paths
// reachable with no session at all, and every /api/v1/admin/* request
// requires a real, live one -- requireSession runs before
// requirePlatformAdmin on all of them (middleware_admin.go's own doc
// comment on requirePlatformAdmin says so, and pins it with a test). This
// constant backs a narrower, code-aware check below instead; see the 401
// branch's own comment for why path alone can't express it.
const ADMIN_API_PREFIX = "/api/v1/admin/";

function readCookie(name: string): string | undefined {
  const prefix = `${name}=`;
  // slice past the first "=" only -- split("=")[1] would truncate any
  // value containing "=", which base64 tokens routinely do (padding).
  return document.cookie
    .split("; ")
    .find((row) => row.startsWith(prefix))
    ?.slice(prefix.length);
}

export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);

  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (method !== "GET" && method !== "HEAD") {
    const token = readCookie("csrf_token");
    if (token) headers.set("X-CSRF-Token", token);
  }

  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin",
  });

  if (response.status === 204) {
    return undefined as T;
  }

  const text = await response.text();
  let parsed: unknown = undefined;
  try {
    parsed = text ? JSON.parse(text) : undefined;
  } catch {
    parsed = undefined;
  }

  if (!response.ok) {
    const envelope = parsed as
      | {
          error?: {
            code?: string;
            message?: string;
            details?: Record<string, unknown>;
          };
        }
      | undefined;

    // A 401 from anywhere other than the exempt pre-auth paths means the
    // session this tab thought it had is no longer valid -- revoked
    // capabilities, an expired session, a signed-out-elsewhere member -- and
    // the spec calls for reacting immediately, not waiting for whatever
    // queries happen to be mounted to notice on their own next refetch (see
    // RequireAuth, which only catches this on a remount). This fires before
    // the ApiError below is thrown, and its effect (the real handler clears
    // the query cache and navigates to /sign-in) doesn't depend on that
    // throw completing.
    //
    // /api/v1/admin/* is carved out of that rule, not by path like the
    // pre-auth prefixes above, but by code: requireSession is the only
    // middleware anywhere in that subtree that can say the session itself is
    // gone, and it always does so as UNAUTHENTICATED (middleware_session.go).
    // Every other 401 there is requirePlatformAdmin's or requireAdminGrant's
    // own layer answering on top of a session that is still perfectly good
    // -- ADMIN_REAUTH_REQUIRED when the 30-minute grant has lapsed,
    // INVALID_CREDENTIALS when a re-entered password is wrong
    // (admin_handlers.go's handleAdminSession, admin_reauth.go's Verify).
    // AdminGate exists specifically to show those inline instead of bouncing
    // the operator to /sign-in, which a path-based exemption can't express:
    // it would have to swallow this subtree's own UNAUTHENTICATED too, and
    // that 401 -- a genuinely dead session -- must reach the handler exactly
    // like it does everywhere else.
    const isAdminLayerUnauthorized =
      path.startsWith(ADMIN_API_PREFIX) && envelope?.error?.code !== "UNAUTHENTICATED";
    if (response.status === 401 && !isPreAuthRequest(path) && !isAdminLayerUnauthorized) {
      unauthorizedHandler?.();
    }

    throw new ApiError(
      response.status,
      envelope?.error?.code ?? "UNKNOWN",
      envelope?.error?.message ??
        `Request failed with status ${response.status}.`,
      envelope?.error?.details ?? {},
    );
  }

  // A 2xx response whose body is missing or not JSON is not something a
  // caller can safely treat as T — hand back an ApiError instead of a type
  // lie (`undefined` cast to T) that only fails far from here.
  if (parsed === undefined) {
    throw new ApiError(
      response.status,
      "INVALID_RESPONSE",
      "The server returned a non-JSON body.",
    );
  }

  return parsed as T;
}
