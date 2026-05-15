# Engineering Audit — netio (`github.com/atendi9/netio`)

> Read-only audit. Branch audited: `fix/header-memory-aliasing`.
> Verified with `go build ./...` (clean), `go vet ./...` (clean),
> `go test ./... -cover` (all pass; coverage: netio 97.0%, cors 100%,
> e2e has no statements, `e2e/cmd` 0.0%). `go tool cover` total: 93.6%.

netio is a hand-rolled HTTP/1.1 server (own request parser, router, response
writer) — i.e. it re-implements security-sensitive code that `net/http`
normally provides. The audit weights the parser/connection paths accordingly.

## Summary
- critical: 1 · high: 5 · medium: 7 · low: 6
- Top risks:
  - Per-request `Context` (including the request `body` and `header` byte
    slices) is returned to a `sync.Pool` and `reset()`-truncated while a
    handler may still hold references — use-after-free style data corruption,
    especially with goroutines or retained slices.
  - The hand-written HTTP parser omits protections that `net/http` has:
    request-line/URI/header size limits, header-count limits, malformed
    chunk-size handling, and request smuggling defenses (both
    `Content-Length` and `Transfer-Encoding` accepted; only exact
    `"chunked"` matched).
  - `parseHeaders` ignores read errors and treats a truncated header block as
    a complete request.

## Findings

### Critical

- [concurrency/correctness] `netio.go:256-317` + `context.go:78-91` —
  `serve()` gets a `*Context` from `ctxPool`, runs the handler chain, then
  unconditionally `ctxPool.Put(ctx)`. On the next keep-alive request the same
  `Context` is `reset()`, which does `c.body = c.body[:0]`, `c.header[:0]`,
  etc. and then `parseRequest` overwrites the backing arrays. Any handler
  that (a) spawns a goroutine using `c`, `c.Body()`, params or headers, or
  (b) stores `c.Body()` / a `Params` value beyond the handler return, will
  observe its data silently mutated or zeroed by a later request on the same
  connection. `Body()` returns the internal slice directly (`context.go:126-128`),
  and `Params/Query/Header` return `string(...)` copies (safe) — but `Body()`
  and the `*Context` itself are not safe to retain. This is a data-corruption
  / cross-request data-leak bug, not just a style issue.
  Fix: document loudly that `*Context` and `Body()` are valid only for the
  duration of the handler; better, return a copy from `Body()`, and do not
  pool the `Context` if any handler may outlive the request (or hand each
  request a fresh `Context` and only pool the byte buffers).

### High

- [security] `parse.go:34-55` — `parseRequestLine` imposes no length limit on
  the request line or URI, and `parse.go:57-72` `parseHeaders` imposes no
  limit on the number of headers or the size of each header. A malicious peer
  can stream an unbounded request line / header block; `bufio.Reader.ReadBytes`
  will accumulate it and OOM the process. `maxBodySize` only guards the body.
  Fix: cap request-line length, per-header length, and header count; abort
  with `parseBadReq` / 431 when exceeded.

- [security] `parse.go:96-120` — request smuggling surface: `parseBody`
  accepts a `Content-Length` body, and *separately* a `Transfer-Encoding`
  body, but never rejects a request that carries *both* (RFC 7230 §3.3.3
  requires rejecting that), and matches transfer-encoding only with
  `bytes.Equal(te, "chunked")` — `"Chunked"`, `"chunked, gzip"`, or
  `"identity, chunked"` all silently fall through to "no body". Behind a proxy
  this is a classic request-smuggling primitive.
  Fix: reject requests with both headers; case-insensitively detect `chunked`
  as the final encoding; reject unknown transfer codings.

- [error-handling] `parse.go:57-72` — `parseHeaders` returns on *either* a
  blank line *or* a read error (`... || err != nil`), with no signal to the
  caller. A connection that dies mid-headers is parsed as a complete, valid
  request and dispatched to a handler with a truncated header set.
  Fix: distinguish "end of headers" from "read error"; on error return a
  parse failure so `serve` can send 400 / close.

- [correctness] `router_node.go:55-77` — `findMethod` does a first pass for an
  exact static child and a second pass for a param child, but it does **not
  backtrack**: if a static segment matches at depth N and the route then
  dead-ends at depth N+1, the function returns `nil,false` even when a param
  sibling at depth N would have matched the full path. Routes like
  `GET /users/new` + `GET /users/:id/posts` can become unreachable depending
  on registration order.
  Fix: on a failed recursive descent into the static child, fall through and
  also try the param child (real backtracking), trimming any params appended
  on the abandoned branch.

- [resource/concurrency] `netio.go:192-204` — `acceptLoop` spawns one
  unbounded goroutine per accepted connection (`go func(){ a.serve(conn) }()`)
  with no concurrency limit and no idle-connection cap. A slowloris-style
  client opening many keep-alive connections (each held 60s by
  `defaultReadTimeout`) exhausts goroutines/FDs. There is also no total
  connection limit.
  Fix: bound concurrency with a semaphore / worker pool; add an
  idle-connection cap and a per-request (not just per-read) deadline.

### Medium

- [correctness] `context.go:344-365` — `HeaderSet`/`HeaderAppend` compare keys
  case-insensitively but **store** the original-cased key on first insert,
  then `writeResponseWithHeaders` emits keys verbatim. Mixed-case duplicates
  from different call sites are deduplicated, but a header first added as
  `content-type` then "set" as `Content-Type` keeps the lowercase form. Minor,
  but inconsistent header casing can trip strict clients/proxies.

- [correctness] `parse.go:128-159` — `parseChunked` ignores the trailing
  `\r\n` read results (`r.ReadBytes('\n')` return values discarded) and does
  not validate chunk extensions or trailers; a malformed chunk framing is
  partially tolerated. It also does no per-chunk-size sanity bound before
  `make([]byte, size)` beyond the cumulative `maxBodySize` check — a single
  huge `size` line is allocated in one `make` before the limit is re-checked
  (the check is `len(body)+int(size) > maxBodySize`, so it *is* caught, but on
  a 32-bit platform `int(size)` from a 64-bit parse can overflow negative).
  Fix: validate `size` is non-negative and within `maxBodySize` *before*
  `make`; check the `\r\n` terminators.

- [correctness] `netio.go:307-308` — after the handler chain, if nothing was
  written `serve` sends `204 No Content`. But a handler that called
  `c.Status(500)` without sending a body still results in `204`, discarding
  the intended status. `204` is also wrong for many "handler forgot to
  respond" cases (`500` would be safer).
  Fix: if `c.status` was explicitly set, honor it; otherwise consider `500`
  for the "no handler output" case.

- [observability] `context.go:228-232`, `:297-298`, `:301-306` — `Send`,
  `JSON`, and `SendFileFromReader` each construct a brand-new logger via
  `NewDefaultLogger(c.appName)` on *every response*, ignoring the
  app-configured `App.logger`. Per-call logger allocation in the hot path, and
  a custom `Logger` passed to `New` is silently bypassed for response logging.
  Fix: thread the `App.logger` onto the `Context` and reuse it.

- [security/observability] `response_writer.go:74-77` — every response logs
  its full header block to stdout unconditionally. Response headers can
  contain `Set-Cookie` / `Authorization`-echo / tokens; unconditional header
  logging is a PII/secret-leak risk and is noisy in production.
  Fix: make response logging opt-in / leveled, and redact sensitive headers.

- [correctness] `context.go:139-149,151-157` — `QueryParser`/`ParamsParser`/
  `ReqHeaderParser` -> `mapToStruct` only handles `reflect.String` fields;
  `int`, `bool`, `float`, slices, and nested structs are silently skipped. A
  caller binding `Page int` gets `0` with no error.
  Fix: support the common scalar kinds, or return an error for unsupported
  field types instead of silently ignoring them.

- [testing] `e2e/cmd/main.go` 0% covered and `parseBody`/`parseChunked`
  failure branches (87.5% / 85.0%) uncovered — the malformed-input paths of a
  hand-written parser are the highest-risk code and the least tested. `New`
  (86.4%) and `Listen` (85.7%) bind-failure branches also uncovered.
  Fix: add table-driven tests feeding malformed request bytes directly to
  `parseRequest` (truncated headers, bad chunk size, negative content-length,
  oversized line, both CL+TE).

### Low

- [correctness] `split.go:7-10` — `splitBytes` returns `nil` for input of
  length `<= 1`, so the root path `"/"` produces no segments and
  `findMethod` matches `n.handlers` at the root — works, but a path like `""`
  is treated identically to `"/"`. Edge behavior is undocumented.

- [correctness] `netio.go:43-48` — `MaxBodySize.String()` defaults to
  `"15 MB"` but the doc/exported `AppConfig.MaxBodySize` gives no hint of the
  default; and an empty `MaxBodySize` silently becomes 15 MB rather than
  "unlimited". Document the default.

- [api] `context.go:313-316` — `Param(key)` is documented as "alias for Params
  without default value support" but `Params` already returns `""` when absent
  and the default is variadic — `Param` is redundant. Harmless duplication.

- [clarity] `logger.go:25-32` — `App.log` checks `if a.logger != nil` then
  falls back to `fmt.Print`; but `New` always assigns a logger, so the
  `fmt.Print` branch is dead code in practice.

- [version-control] `coverage.html`, `coverage.out`, `coverage_int.out` are
  present in the working tree (untracked, and `.gitignore` covers `*.out` /
  `coverage.*`) — fine, but they sit next to source; consider a `tmp/` dir.
  `CLAUDE.md` and `.ai-jail` are untracked tooling files — confirm they are
  intentionally not committed.

- [docs] `router_group.go` `Router` interface uses `Get/Post/...` (title-case)
  while `App` exposes `GET/POST/...` (upper-case). Two casings for the same
  concept across the public API is inconsistent.

## Strengths
- `Shutdown(ctx)` is a genuine graceful shutdown: closes the listener, waits
  on a `sync.WaitGroup` of active connections, and force-closes via
  `closeAllConns()` on context deadline; connection set is mutex-protected.
- `ListenHTTPS` enforces `tls.VersionTLS13` as the minimum — strong default.
- `sync.Pool` for `Context` and response buffers is the right instinct for
  allocation pressure (the lifetime bug above is the caveat, not the pattern).
- `response_writer.go` correctly honors RFC 7230 by not emitting
  `Content-Length` together with `Transfer-Encoding`.
- The `cors` subpackage is well-built: 100% covered, sets `Vary` correctly,
  refuses `*` origin together with credentials, and reflects request headers
  only when configured to.
- Good overall test coverage (97% of the core package) with both unit and
  integration/e2e suites; tests are deterministic and network-bound only in
  the explicit e2e package.
- `detectContentType` sensibly upgrades JSON detection beyond
  `http.DetectContentType`.
