# DevGate

DevGate is a production-oriented API gateway written in Go.

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
