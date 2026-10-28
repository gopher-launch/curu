# Runtime configuration

The Curu server reads runtime settings from environment variables and rejects malformed values before listening. The current shell has no secrets and exposes no product-data routes.

| Variable | Default | Contract |
| --- | --- | --- |
| `CURU_LISTEN_ADDRESS` | `127.0.0.1:8080` | TCP `host:port`; use an explicit non-loopback address only inside the intended deployment boundary. Port `0` is supported for disposable tests. |
| `CURU_SHUTDOWN_TIMEOUT` | `10s` | Positive Go duration allowed for graceful HTTP shutdown. |
| `CURU_READ_HEADER_TIMEOUT` | `5s` | Positive Go duration allowed to read request headers. |
| `CURU_IDLE_TIMEOUT` | `60s` | Positive Go duration for idle keep-alive connections. |

Configuration for PostgreSQL, trusted JWTs, owner authorization, public origin, CSRF, and proxy behavior is intentionally absent until the roadmap tasks that implement those boundaries. Because no product routes exist, missing future security configuration cannot expose a usable private application. The only successful unauthenticated responses are coarse liveness and readiness checks; the deployment platform is responsible for restricting readiness to its platform network.
