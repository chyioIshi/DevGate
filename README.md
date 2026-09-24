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
