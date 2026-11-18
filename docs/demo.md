# Local failure walkthrough

[Back to README](../README.md)

This walkthrough uses the [Quick start](../README.md#quick-start) and
[`examples/local.yaml`](../examples/local.yaml). It demonstrates a failed
upstream connection and recovery through the route circuit breaker.

Use a fresh DevGate process and keep the default retry and circuit-breaker
environment settings. Active and passive health checks are not configured in
this example, so endpoint health does not independently reject requests.

## 1. Verify the healthy path

With the backend and DevGate running:

```bash
curl -i http://127.0.0.1:8080/api/hello.txt
```

Expect `200 OK`, `Hello from upstream`, and an `X-Request-ID` response header.
The proxy strips `/api` before contacting the backend.

## 2. Stop the backend

Press `Ctrl+C` in the Python backend terminal. Leave DevGate running.

Send six sequential requests from the request terminal:

```bash
for attempt in 1 2 3 4 5 6; do
  curl -sS -o /dev/null -w '%{http_code}\n' \
    http://127.0.0.1:8080/api/hello.txt
done
```

Expected output:

```text
502
502
502
502
502
503
```

Each of the first five gateway requests encounters a transport error. The
default retry limit allows two attempts per eligible request; the circuit
breaker counts the final request outcome once, not each attempt. Five
consecutive failures open the circuit. The sixth request returns `503` without
contacting the backend.

These requests stay within the demo bucket's initial burst of 20. Additional
rapid traffic may produce `429`, which is rate limiting rather than an
upstream or circuit-breaker result.

## 3. Inspect observability

```bash
curl -sS http://127.0.0.1:8080/metrics
curl -i http://127.0.0.1:8080/healthz
```

Metrics distinguish HTTP outcomes and circuit-breaker rejections. JSON logs
include the route, status, and request ID for each routed request.
`/healthz` still returns `200`: it reports process liveness, not backend health.

## 4. Restore the backend

Restart the backend from the repository root:

```bash
python3 -m http.server 9000 --bind 127.0.0.1 --directory examples/backend
```

The default circuit-open timeout is 30 seconds. Once that interval has elapsed
since the circuit opened, the next request is allowed through as a half-open
probe. If it succeeds, the circuit closes:

```bash
curl -i http://127.0.0.1:8080/api/hello.txt
```

Expect `200 OK` again. Requests before the timeout expires still receive `503`;
the circuit does not probe continuously in the background.

Stop both servers with `Ctrl+C` when finished.
