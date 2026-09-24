# DevGate

DevGate is a production-oriented API gateway written in Go.

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
