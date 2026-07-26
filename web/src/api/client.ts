// The single entry point for talking to the Go service. Every request goes
// through here so that credentials, the CSRF header and error decoding are
// handled in exactly one place.

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

function readCookie(name: string): string | undefined {
  return document.cookie
    .split("; ")
    .find((row) => row.startsWith(`${name}=`))
    ?.split("=")[1];
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
