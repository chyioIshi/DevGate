# DevGate

DevGate is a production-oriented API gateway written in Go.

## Path routing

Each route configures exactly one path matcher. A prefix matcher accepts the
configured path and its child path segments:

```yaml
path_prefix: /api/users
```

It matches `/api/users` and `/api/users/42`, but not `/api/users-v2`. An exact
matcher accepts only one complete path:

```yaml
path_exact: /api/status
```

When an exact matcher and a prefix matcher of the same length both match, the
exact route takes precedence. `strip_path_prefix` is supported only with
`path_prefix`.

## Header routing

Routes can require exact values for request headers:

```yaml
routes:
  - name: production-users
    protocol: http
    path_prefix: /api/users
    header_matches:
      - name: X-Environment
        exact: production
    upstream_url: http://users-service:8080
```

Header names are case-insensitive, while values are case-sensitive. All
configured header matches must succeed. A matched header must be present with
exactly one value; missing or repeated values do not match. A route without
`header_matches` accepts any request headers.

Matching uses the incoming request headers and happens before request header
transformations are applied.

## Route priority

Use `priority` to choose between matching routes with the same path matcher.
Higher values take precedence, the default is `0`, and negative values are
allowed:

```yaml
routes:
  - name: production-users
    protocol: http
    path_prefix: /api/users
    header_matches:
      - name: X-Environment
        exact: production
    priority: 100
    upstream_url: http://production-users-service:8080

  - name: default-users
    protocol: http
    path_prefix: /api/users
    priority: 0
    upstream_url: http://users-service:8080
```

A request with `X-Environment: production` uses `production-users`; other
requests use `default-users`. Path specificity is evaluated first: a longer
matching path wins, and an exact path wins over an equal-length prefix.
Priority is compared only after those rules.

Routes with the same path matcher, the same priority, and overlapping method,
host, and header conditions are rejected as ambiguous. Assign different
priorities when the overlap is intentional.

## Upstream pools

An upstream route can distribute requests across a static pool of backend
endpoints:

```yaml
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream:
      load_balancing: round_robin
      discovery:
        static:
          endpoints:
            - url: http://users-service-1:8080
            - url: http://users-service-2:8080
```

Supported load-balancing policies:

- `round_robin` selects endpoints in cyclic order and is the default when
  `load_balancing` is omitted;
- `random` selects an endpoint uniformly at random for each request;
- `least_requests` selects the endpoint with the fewest active requests and
  uses round-robin ordering to break ties.

For `least_requests`, an endpoint remains active until its response body is
closed, so streaming HTTP and WebSocket traffic contributes to its load for
the lifetime of the proxied request.

A static pool must contain at least one absolute HTTP or HTTPS URL.

The legacy `upstream_url` field remains supported for single-endpoint routes.
DevGate normalizes it internally to a round-robin pool containing one endpoint.
`upstream` and `upstream_url` are mutually exclusive.

### Connection pooling

DevGate reuses upstream HTTP connections through a shared keep-alive pool.
Its capacity and idle lifetime can be configured with environment variables:

```dotenv
DEVGATE_UPSTREAM_MAX_IDLE_CONNS=100
DEVGATE_UPSTREAM_MAX_IDLE_CONNS_PER_HOST=20
DEVGATE_UPSTREAM_MAX_CONNS_PER_HOST=100
DEVGATE_UPSTREAM_IDLE_CONN_TIMEOUT=90s
```

`MAX_IDLE_CONNS` limits idle connections across all upstream hosts, while
`MAX_IDLE_CONNS_PER_HOST` limits idle connections retained for one host.
`MAX_CONNS_PER_HOST` bounds active, idle, and dialing connections for each
host. All limits and the idle timeout must be positive.

### DNS discovery

A route can periodically resolve one logical HTTP target into an endpoint for
each distinct IPv4 or IPv6 address returned by the system resolver:

```yaml
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream:
      load_balancing: round_robin
      discovery:
        dns:
          url: http://users.service.local:8080
          refresh_interval: 30s
          lookup_timeout: 2s
```

`static` and `dns` are mutually exclusive. DNS discovery requires exactly one
logical target and currently supports only the `http` scheme. Its port, path,
and query are preserved when the hostname is replaced with each resolved IP
address.

DNS routes start fail-closed: requests receive `503 Service Unavailable` until
the first successful lookup publishes an endpoint snapshot. A lookup error
keeps the last successfully applied snapshot. A successful empty DNS response
publishes an empty snapshot and returns the route to fail-closed behavior.
Active and passive health state, endpoint membership, and load balancing are
updated together whenever a new snapshot is published.

When a refresh removes an endpoint, new requests stop selecting it
immediately. Requests already using that endpoint are allowed to finish, and
its unused keep-alive connections expire according to
`DEVGATE_UPSTREAM_IDLE_CONN_TIMEOUT`.

DNS discovery exports the following Prometheus metrics:

- `devgate_dns_discovery_refreshes_total{route,outcome}` counts refresh
  attempts; `outcome` is either `success` or `failure`;
- `devgate_dns_discovery_resolved_endpoints{route}` reports the number of
  endpoints in the latest successfully applied snapshot.

## Active health checks

An upstream pool can periodically probe every configured endpoint:

```yaml
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream:
      load_balancing: least_requests
      discovery:
        static:
          endpoints:
            - url: http://users-service-1:8080
            - url: http://users-service-2:8080
      active_health_check:
        path: /healthz
        interval: 10s
        timeout: 2s
        healthy_threshold: 2
        unhealthy_threshold: 3
        max_concurrent_probes: 4
```

DevGate sends `GET` probes directly to each endpoint, bypassing request retries
and the route circuit breaker. A `2xx` response is successful; transport
errors, timeouts, and other status codes are failures. Thresholds count
consecutive outcomes before changing an endpoint's state.

New endpoints start healthy. Once an endpoint becomes unhealthy, it is removed
from load balancing until it reaches the healthy threshold again. If no healthy
endpoint remains, DevGate fails closed with `503 Service Unavailable` without
attempting a data-plane upstream request.

Active health checks export the following Prometheus metrics:

- `devgate_active_health_check_probes_total{route,target,outcome}` counts probe
  results; `outcome` is either `success` or `failure`;
- `devgate_active_health_check_endpoint_healthy{route,target}` reports the
  current endpoint state as `1` for healthy or `0` for unhealthy.

Targets use redacted URLs so configured passwords are not exposed by the
metrics endpoint. Error messages are intentionally excluded from labels to
keep label cardinality bounded.

## Passive health checks

An upstream pool can temporarily eject endpoints based on real proxied request
outcomes:

```yaml
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream:
      load_balancing: round_robin
      discovery:
        static:
          endpoints:
            - url: http://users-service-1:8080
            - url: http://users-service-2:8080
      passive_health_check:
        consecutive_failures: 3
        ejection_duration: 30s
```

Transport errors and final `5xx` responses count as failures. A successful
non-`5xx` response resets the consecutive-failure counter. Requests canceled
by the downstream client or gateway context do not affect endpoint health.

Once an endpoint reaches `consecutive_failures`, DevGate removes it from load
balancing for `ejection_duration`. It becomes eligible again automatically
after that period. Passive health observes the final result after retries, and
requests rejected by the route circuit breaker are not counted as endpoint
failures.

When active and passive health checks are both configured, an endpoint must be
healthy according to both mechanisms to receive traffic. If no eligible
endpoint remains, DevGate fails closed with `503 Service Unavailable` without
attempting an upstream request.

## Direct responses

A route can return a static HTTP response without forwarding the request to an
upstream:

```yaml
routes:
  - name: maintenance
    path_prefix: /api
    direct_response:
      status: 503
      body: service temporarily unavailable
    response_headers:
      set:
        Retry-After: "120"
```

Each route configures exactly one action: `upstream` (or legacy
`upstream_url`), `direct_response`, or `redirect`. The `protocol` field applies
only to upstream routes and must be omitted for a direct response.

Direct-response status codes must be between `200` and `599`. Responses with
status `204`, `205`, or `304` cannot configure a body. Response-header
transformations and rate limiting remain available, while proxy-only settings
such as request-header transformations, path-prefix stripping, request
timeouts, and request-body limits are rejected.

## Redirects

A route can redirect a client without forwarding the request to an upstream:

```yaml
routes:
  - name: legacy-api
    path_exact: /legacy
    redirect:
      status: 308
      location: /api/v2
    response_headers:
      set:
        Cache-Control: no-store
```

Redirect status must be `301`, `302`, `303`, `307`, or `308`. The location can
be a relative reference or an absolute HTTP or HTTPS URL. Response-header
transformations and rate limiting are supported, but the `Location` response
header is owned by the redirect action and cannot be set or removed by a
header transformation. Proxy-only settings are rejected.

## Custom error responses

DevGate can replace its default plain-text errors with custom bodies and
headers. Top-level responses apply to errors generated by the gateway itself:

```yaml
error_responses:
  404:
    body: '{"error":"route not found"}'
    headers:
      Content-Type: application/json
  405:
    body: '{"error":"method not allowed"}'
    headers:
      Content-Type: application/json
  500:
    body: '{"error":"internal server error"}'
    headers:
      Content-Type: application/json
```

Global responses support `404`, `405`, and `500`. They cover unmatched routes,
unsupported methods (including `/healthz` and `/metrics`), internal routing
failures, recovered panics, and request-ID generation failures.

A route can separately customize errors produced while processing that route:

```yaml
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
    max_request_body_bytes: 10485760
    rate_limit:
      requests_per_second: 100
      burst: 200
    error_responses:
      413:
        body: '{"error":"request body too large"}'
        headers:
          Content-Type: application/json
      429:
        body: '{"error":"rate limit exceeded"}'
        headers:
          Content-Type: application/json
      502:
        body: '{"error":"upstream unavailable"}'
        headers:
          Content-Type: application/json
```

Route responses support `413`, `429`, `502`, `503`, and `504`. Status `413`
requires a request-body limit, `429` requires rate limiting, and upstream
errors require an upstream action. Responses not explicitly configured retain
the standard plain-text fallback.

Custom bodies are written exactly as configured and are omitted for `HEAD`
requests. DevGate rejects invalid headers, duplicate header names with
different casing, and transport-owned headers such as `Content-Length`,
`Transfer-Encoding`, `Connection`, and `X-Request-ID`.

## Trusted proxies

DevGate does not trust incoming `X-Forwarded-For` headers by default. Configure
the IP networks of reverse proxies and load balancers that connect directly to
DevGate with a comma-separated list of canonical CIDR prefixes:

```dotenv
DEVGATE_TRUSTED_PROXY_CIDRS=10.0.0.10/32,2001:db8::10/128
```

Only configure infrastructure controlled by you. Do not add client networks or
use a prefix that trusts every address, such as `0.0.0.0/0` or `::/0`.

For requests from an untrusted peer, DevGate discards the supplied forwarding
chain and forwards only the peer IP. For requests from a trusted peer, DevGate
walks the chain from right to left, keeps the first untrusted client boundary
and the trusted proxy suffix, and discards spoofed values to the left of that
boundary.

## Router benchmarks

Run the router benchmarks without executing the regular test suite:

```shell
go test -run '^$' -bench '^BenchmarkRouter' -benchmem ./internal/router
```

Use multiple runs when comparing changes so that normal measurement noise is
visible:

```shell
go test -run '^$' -bench '^BenchmarkRouter' -benchmem -count=5 ./internal/router
```

The suite covers successful matching across different routing-table sizes, the
method-not-allowed lookup path, and concurrent matching against an immutable
routing table.
