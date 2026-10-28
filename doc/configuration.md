# Runtime configuration

The Curu server reads runtime settings from environment variables and rejects malformed values before listening. It exposes no product-data routes yet, but startup now requires a migrated compatible database.

| Variable | Default | Contract |
| --- | --- | --- |
| `CURU_DATABASE_URL` | required | PostgreSQL URL for the restricted runtime role. Must include user, host, and database. Startup reads migration history and never migrates or seeds. Select the deployment-required TLS mode in the URL. |
| `CURU_LISTEN_ADDRESS` | `127.0.0.1:8080` | TCP `host:port`; use an explicit non-loopback address only inside the intended deployment boundary. Port `0` is supported for disposable tests. |
| `CURU_SHUTDOWN_TIMEOUT` | `10s` | Positive Go duration allowed for graceful HTTP shutdown. |
| `CURU_READ_HEADER_TIMEOUT` | `5s` | Positive Go duration allowed to read request headers. |
| `CURU_IDLE_TIMEOUT` | `60s` | Positive Go duration for idle keep-alive connections. |

Migration-only settings and commands are documented in [`database-operations.md`](database-operations.md); migration credentials are never accepted by the server. Configuration for trusted JWTs, owner authorization, public origin, CSRF, and proxy behavior remains absent until the roadmap tasks that implement those boundaries. Because no product routes exist, missing future request-security configuration cannot expose a usable private application. The only successful unauthenticated responses are coarse liveness and readiness checks; the deployment platform is responsible for restricting readiness to its platform network.
