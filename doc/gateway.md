# Gateway and browser deployment contract

Curu runs behind one trusted gateway hop. This document describes the implemented provider-neutral boundary; it does not prescribe or provision a gateway, identity provider, TLS service, or cloud platform.

## Public browser hop

- The gateway terminates mandatory public HTTPS at the exact origin configured as `CURU_PUBLIC_ORIGIN`. It owns login, refresh, browser-session cookies, and logout. Gateway cookies should be `Secure`, `HttpOnly`, and use the narrowest suitable `SameSite` and path settings.
- The gateway removes every browser-supplied `Authorization`, forwarding-identity, `Forwarded`, and `X-Forwarded-*` header. It then injects exactly one `Authorization: Bearer <JWT>` value containing an access token for Curu, never an ID token.
- Optional `CURU_LOGIN_URL` and `CURU_LOGOUT_URL` values let Curu display gateway navigation. Curu does not clear gateway cookies, replay failed mutations, or implement provider logout.
- Network policy prevents direct public access to Curu. The platform protects the private hop with its selected TLS or equivalent authenticated private-network control and documents that choice outside this repository.

## Forwarding and origin rules

Each configured `CURU_TRUSTED_PROXY_CIDRS` entry identifies a possible immediate gateway peer. The gateway overwrites `X-Forwarded-For` with the single public client IP; appended chains are ignored. Curu ignores forwarding addresses from peers outside those CIDRs. The configured public origin is never inferred from `Host`, `Forwarded`, or `X-Forwarded-*`; malformed internationalized labels and noncanonical numeric host spellings that a browser would reject or reinterpret are refused at startup.

Every unsafe browser request must carry the exact configured HTTPS `Origin`, a matching Curu CSRF cookie/token pair, and, when present, `Sec-Fetch-Site: same-origin`. The `__Host-curu_csrf` cookie is Curu-owned, secure, HTTP-only, same-site lax, host-only, and valid for `/`. Forms use `csrf_token`; HTMX uses `X-CSRF-Token`. Every authorized product request body, including a safe-method body, is capped at 1 MiB by the shared boundary, and receiving handlers must further allowlist their content type and validate fields. Product routes do not enable CORS.

Safe authorized responses establish a CSRF token without rotating a still-valid one. A rejected request never claims success. An HTMX `401` has `HX-Reswap: none` and triggers `curu:authentication-required`, allowing the shared page layer to retain the editor DOM and show the configured sign-in action. The browser deliberately starts a new sign-in navigation; Curu neither redirects nor replays the failed mutation.
