# Findings — root causes and upstream references

Empirical results are published per run as GitHub Releases — `MATRIX.md` and
`results.json` on the [latest release](https://github.com/signadot/context-propagation-matrix/releases/latest),
raw header dumps per test in its `raw.tar.gz` — and browsable at
https://contextpropagation.io. This file documents *why* the notable
cells look the way they do, with upstream citations. Runs dated 2026-09-05
(Go build-time weaving variants: 2026-09-07); 39 tracer variants plus 8
no-instrumentation baselines × 16 boundaries ({http, grpc, kafka,
rabbitmq}²); tracer versions as recorded per variant.

## 1. Datadog Java: W3C `baggage` survives only HTTP→HTTP — 1 of 16 boundaries

**Observed** (dd-trace-java 1.65.1, `DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage`
and also with defaults): `baggage` propagated on http→http only; absent on the
other **15** boundaries, including every RabbitMQ combination. `traceparent`,
`tracestate`, `ot-baggage-*`, and `x-datadog-*` propagate on **all 16**.

**Root cause**: Kafka/AMQP producer (and non-HTTP client) instrumentation
injects from the *span* rather than the *current context*
(`defaultPropagator().inject(span, ...)`); baggage lives on the thread-local
context, and `AgentSpan.get()` delegates non-span context keys to
`Context.root()`, so the baggage propagator finds nothing. Reported upstream
in May 2026:
[dd-trace-java#11286](https://github.com/DataDog/dd-trace-java/issues/11286).
Datadog's response confirms scoping: "W3C baggage propagation was specifically
scoped to HTTP to begin with … extending it to other transports is a feature
and not a bug", citing the absence of a W3C spec for non-HTTP carriers; a
second maintainer comment is blunter: "Kafka instrumentation is indeed
dropping all context elements except tracing at injection." A community fix,
[PR #11330](https://github.com/DataDog/dd-trace-java/pull/11330), documented
the root cause precisely ("`AgentSpan.get()` delegates non-span context keys
(like baggage) to `Context.root()` instead of the thread-local
`Context.current()`") but was closed unmerged, never reviewed. Datadog *is*
migrating to context-centric propagation — in its own words, fixing paths
that "carried only the active AgentSpan across the … hand-off, dropping the
rest of the Datadog Context (including baggage)"
([PR #12027](https://github.com/DataDog/dd-trace-java/pull/12027),
[PR #12033](https://github.com/DataDog/dd-trace-java/pull/12033), merged
Jul 2026; same failure class fixed on Spring WebClient in
[#11510](https://github.com/DataDog/dd-trace-java/issues/11510)) — but that
work covers async/HTTP paths only; the messaging inject sites were still
span-centric at 1.65.1, and no issue or PR exists for baggage on gRPC,
RabbitMQ, or JMS boundaries at all — our measurements there appear
unreported. Groundwork exists (context/tracing decoupling in
[PR #8330](https://github.com/DataDog/dd-trace-java/pull/8330) and
[PR #10767](https://github.com/DataDog/dd-trace-java/pull/10767), milestone
1.61.0).

**Contrast**: dd-trace-js 6.14.0, dd-trace-go v2.10.1, dd-trace-dotnet 3.52.0
and datadog-opentelemetry (Rust) 0.5.2 all propagate W3C baggage across **all
16** boundaries — the gap is implementation-specific, not a Datadog design
position.

## 2. Datadog Python: Kafka off by default, RabbitMQ (pika) not covered at all

**Observed** (ddtrace 4.14.0): with default config, *no propagation headers of
any kind* are injected into produced Kafka records (`*→kafka`: everything
absent). Setting `DD_KAFKA_PROPAGATION_ENABLED=true` turns on full W3C
injection *including baggage*, but consumer-side continuation is still broken:
`kafka→*` starts a new trace and drops baggage. RabbitMQ is worse: nothing is
injected or extracted on any `*→rabbitmq` / `rabbitmq→*` leg, flag or no flag.

**Root cause / reference**: Kafka injection is a documented default-off —
`ddtrace.config.kafka["distributed_tracing_enabled"]`, "Default: False", env
`DD_KAFKA_PROPAGATION_ENABLED`
([ddtrace integrations docs](https://ddtrace.readthedocs.io/en/stable/integrations.html);
introduced default-off in
[PR #7434](https://github.com/DataDog/dd-trace-py/pull/7434) "to prevent any
unexpected changes to users' kafka tracing"). The consume-side gap is
acknowledged by Datadog's own engineers —
[PR #7345](https://github.com/DataDog/dd-trace-py/pull/7345): "`kafka.consume`
and `kafka.produce` spans are not connected … because the context isn't used
when the `kafka.consume` span is created" — and has a user-report trail
([#8967](https://github.com/DataDog/dd-trace-py/issues/8967), whose
workaround is manual
`tracer.context_provider.activate(Propagator.extract(...))`;
[#17980](https://github.com/DataDog/dd-trace-py/issues/17980), open, on
batch-activation semantics). Datadog is now migrating consume to
links-instead-of-continuation, gated behind a config
([PR #18667](https://github.com/DataDog/dd-trace-py/pull/18667),
[PR #19858](https://github.com/DataDog/dd-trace-py/pull/19858): "We can't do
that directly because we would break existing semantics"). For RabbitMQ,
dd-trace-py simply has **no pika integration** — its AMQP coverage is the
`kombu` library only (same docs), pika is "in our team's feature backlog and
… not planned for the short term"
([#2047](https://github.com/DataDog/dd-trace-py/issues/2047), 2021;
aio-pika re-requested in
[#17225](https://github.com/DataDog/dd-trace-py/issues/17225), open).
Uninstrumented library ⇒ silent total loss.

**Also — the `ot-baggage-*` split, explained**: dd-trace-py *has*
`ot-baggage-*` code but both inject and extract are gated behind
`DD_TRACE_PROPAGATION_HTTP_BAGGAGE_ENABLED` (default **False**, added v2.4.0
in [PR #6072](https://github.com/DataDog/dd-trace-py/pull/6072) explicitly
for compatibility "with other language DataDog tracing libs e.g. java,
nodejs"); the OpenTracing bridge was removed entirely in
[v3.0.0](https://github.com/DataDog/dd-trace-py/releases/tag/v3.0.0).
dd-trace-rb removed OpenTracing (and with it `ot-baggage-*`) in 2.0
([PR #3443](https://github.com/DataDog/dd-trace-rb/pull/3443); maintainer
confirmation in [#4055](https://github.com/DataDog/dd-trace-rb/issues/4055)).
dd-trace-dotnet **never had it** — a code search for `ot-baggage` returns
zero results. And the style is undocumented everywhere: Datadog's
[propagation reference](https://docs.datadoghq.com/tracing/trace_collection/trace_context_propagation/)
lists no ot-baggage format — the headers dd-trace-java/js/go emit are legacy
behavior of their `datadog` style, not a documented, configurable format.

## 3. OpenTelemetry Python: Kafka-inbound loses everything — but RabbitMQ is perfect

**Observed** (instrumentation 0.65b0): kafka→* all start a new trace; baggage
and tracestate lost. Every other boundary — including all eight RabbitMQ
combinations via `opentelemetry-instrumentation-pika` — propagates everything.

**Root cause**: `opentelemetry-instrumentation-confluent-kafka` wraps `poll()`
and creates a consume span with the producer context as a *link*
(`_create_new_consume_span` calls `start_span(..., links=links)`), but never
calls `context.attach()` — the extracted context (and its baggage) is never
made current for the code that processes the message. Long-standing upstream
issues:
[#1674](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1674)
(open since Feb 2023, exact symptom),
[#1962](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1962)
(maintainer meta-issue; item 5 is "Not correct extraction of remote context
for Consumer"),
[#1732](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1732)
(kafka-python sibling, closed as duplicate of
[#3026](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3026),
the aiokafka variant that asks for `context.attach(extracted_context)`
verbatim). The only fix attempt,
[PR #1678](https://github.com/open-telemetry/opentelemetry-python-contrib/pull/1678),
was closed unmerged by the stale bot; maintainers' stated position
([#3465](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3465))
is that manual context attach is the intended usage. The pika instrumentation
demonstrates the correct design in the same ecosystem: its per-message
callback wrapper extracts *and attaches* the context before invoking the
handler. The failure is a per-instrumentation design choice, not a language
limitation. No upstream issue names the *baggage* loss specifically —
candidate for filing.

## 4. OpenTelemetry .NET: links instead of parents — Kafka drops all, RabbitMQ splits the verdict

**Observed** (auto-instrumentation 1.16.0): kafka→* starts a new trace with
baggage/tracestate absent. rabbitmq→* **continues the trace** (traceparent +
tracestate survive) **but baggage is dropped**. All `*→kafka` and
`*→rabbitmq` injection works, baggage included.

**Root cause (Kafka)**: the auto-instrumentation deliberately stopped using
the message-creation context as the consumer span's parent in 1.7.0 —
[opentelemetry-dotnet-instrumentation#3434](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/issues/3434)
implemented by [PR #3438](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/pull/3438)
("Do not use message creation context as a parent for consumer spans for
`Confluent.Kafka` client instrumentation") — following the OTel
[messaging semantic conventions](https://github.com/open-telemetry/semantic-conventions/blob/main/docs/messaging/messaging-spans.md),
which "use span links as the default mechanism to correlate producers and
consumer(s)" (rule traced to
[OTEP-0220](https://github.com/open-telemetry/opentelemetry-specification/blob/main/oteps/trace/0220-messaging-semantic-conventions-span-structure.md);
parent-child re-allowed as an option by
[semantic-conventions#1414](https://github.com/open-telemetry/semantic-conventions/pull/1414)).
Same-symptom reports:
[opentelemetry-dotnet-contrib#3995](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/3995)
(**open**; a maintainer comments "no bug here… as long as we are not
switching to the modern [semconv] version there is nothing to change") and
[#2351](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/2351)
(closed by-design). No upstream issue names the Kafka *baggage* loss —
candidate for filing.

**Root cause (RabbitMQ)**: RabbitMQ.Client 7.x ships *native*
`ActivitySource` instrumentation
([rabbitmq-dotnet-client#1261](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1261));
its `DefaultContextExtractor` calls `ExtractTraceIdAndState(...)` only — the
word "baggage" does not appear in the propagation code. Baggage was
addressed only via an opt-in companion package
([PR #1528](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1528):
"Making it possible to override the default context propagation … to have
proper OpenTelemetry Baggage propagation"), and that package,
`RabbitMQ.Client.OpenTelemetry`, has **never shipped stable** — only
1.0.0-rc.1/rc.2 on NuGet
([#1728](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/1728),
open since Nov 2024). The OTel auto-instrumentation likewise tracks RabbitMQ
deliver-span baggage as an unbuilt feature
([opentelemetry-dotnet-instrumentation#3662](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/issues/3662)).
Version caveat: parent-child trace continuity on consume is only the default
since RabbitMQ.Client **7.2.0** (`UsePublisherAsParent`,
[PR #1779](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1779));
on 7.0–7.1 the deliver activity was link-only. Our relay pins 7.2.2.

## 5. OpenTelemetry Ruby: correct on inject, broken on consume (bunny), absent on gRPC server

**Observed** (SDK 1.13.0): http→* and kafka→* (rdkafka) perfect, including
`*→rabbitmq` injection via `opentelemetry-instrumentation-bunny` 0.25.0. But
rabbitmq→* starts a new trace (consume side does not activate the extracted
context), and grpc→* remains a total loss (no gRPC *server* instrumentation
for the plain `grpc` gem).

**Upstream (bunny)**: exact-match issue
[opentelemetry-ruby-contrib#526](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/526)
("Instead of propagating any contexts (including baggage), the bunny
instrumentation drops all contexts in the consumer and starts a new trace"),
**open since June 2023** with no fix PR. Maintainers describe it as
links-by-design per the OTel messaging spec
([discussion #1098](https://github.com/open-telemetry/opentelemetry-ruby/discussions/1098));
mechanically, `with_process_span` parents the process span on the
instrumentation's *own* re-injected local headers and demotes the producer
context to a span link
([patch_helpers.rb](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/bunny/lib/opentelemetry/instrumentation/bunny/patch_helpers.rb)).

**Upstream (gRPC)**: the contrib gRPC instrumentation (donated from
[hibachrach/opentelemetry-instrumentation-grpc](https://github.com/hibachrach/opentelemetry-instrumentation-grpc)
via [PR #1164](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1164),
Sep 2024, answering a request open since
[2020](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/10))
is explicitly client-only:
["Right now, the gem only instruments outbound requests to gRPC services"](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/grpc/README.md).
It *is* bundled in `opentelemetry-instrumentation-all`, so the accurate
statement is "loaded but client-only", and even client injection requires
manually wiring the interceptor into stubs. Server spans exist only for the
[Gruf framework](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/gruf/README.md).
No upstream issue requests server-side `grpc` gem support — filing candidate.

The bunny consume gap is the third instance of the consume-side failure class
(after OTel Python's confluent-kafka and OTel .NET's Kafka links): injection
is easy to get right, *activation of extracted context for user code* is where
messaging instrumentations diverge.

## 6. Datadog Ruby: no bunny integration

**Observed** (datadog gem 2.42.0): all HTTP/gRPC/Kafka boundaries perfect
(incl. W3C baggage). RabbitMQ: nothing injected on produce; consume starts a
fresh trace on the outbound leg. The
[dd-trace-rb integrations list](https://docs.datadoghq.com/tracing/trace_collection/compatibility/ruby/)
has no bunny entry — AMQP coverage is only via the Sneakers/Kicks worker
framework — and the
[contrib source tree](https://github.com/DataDog/dd-trace-rb/tree/master/lib/datadog/tracing/contrib)
confirms no bunny integration exists in code. **No feature request for bunny
exists upstream at all** — filing candidate. Same class as dd-py/pika:
canonical client, zero coverage.

## 7. PHP: the most fragmented column in the matrix

Three structural findings apply to every PHP variant:

- **No gRPC server in any tracer-supported topology.** The official gRPC PHP
  implementation is client-only
  ([grpc.io quickstart](https://grpc.io/docs/languages/php/quickstart/):
  "You can only create gRPC clients in PHP. Use another language to create a
  gRPC server."). PHP *can* serve gRPC via a separate application server —
  RoadRunner's [gRPC plugin](https://docs.roadrunner.dev/docs/grpc/grpc)
  (Go proxy → PHP workers; the de facto standard) or Swoole-based frameworks
  such as Hyperf — but none of the three tracers' zero-code instrumentation
  covers those paths: dd-trace-php's RoadRunner integration hooks the HTTP
  `PSR7Worker` only (Datadog's own guidance for RoadRunner gRPC is manual
  `dd_trace_method` hooks, see
  [dd-trace-php#805](https://github.com/DataDog/dd-trace-php/issues/805));
  OTel has no roadrunner-grpc PHP instrumentation (RoadRunner's `otel`
  plugin traces in the Go proxy layer, not the PHP worker); Elastic supports
  neither. The harness measures the documented Apache mod_php + CLI worker
  topology, so all grpc→* boundaries are undeliverable (🚫). A
  RoadRunner-based relay with manual instrumentation is a candidate
  follow-up (see the roadmap in DESIGN.md).
- **No tool instruments the gRPC client** (`ext-grpc` + `grpc/grpc`): every
  `*→grpc` cell is header-free for otel, datadog, and elastic alike.
- The relay runs Apache mod_php + two CLI workers — the documented supported
  topology for all three tools (dd-trace-php does not support `php -S`).

Per tool (all on PHP 8.3):

- **Datadog (ddtrace 1.25.0)** is the strongest: HTTP and RabbitMQ
  (php-amqplib) propagate everything including W3C baggage in both directions;
  Kafka *injection* works (`*→kafka` ✅ incl. baggage) but the rdkafka
  *consume* side does not continue context (kafka→*: new trace, baggage
  gone) — the by-now-familiar consume-side class. **Mechanism visible in
  source**
  ([KafkaIntegration.php](https://github.com/DataDog/dd-trace-php/blob/master/src/DDTrace/Integrations/Kafka/KafkaIntegration.php)):
  the consume hook starts a span, attaches the extracted headers to it, and
  calls `close_span()` *inside the `consume()` call* — before application
  code ever sees the message, so downstream calls run outside the extracted
  context. The flag (`DD_TRACE_KAFKA_DISTRIBUTED_TRACING`) defaults to true;
  the gap is the scoping, not the flag. No upstream issue reports it —
  filing candidate. gRPC is the oldest gap:
  [dd-trace-php#916](https://github.com/DataDog/dd-trace-php/issues/916),
  open since 2020, marked "up-for-grabs" by Datadog.
- **OpenTelemetry (ext 1.2.1 + auto-instrumentation)**: consume-side rdkafka
  extraction *works* (kafka→http propagates everything: extract + context
  activation are correct), but **producer injection silently no-ops when the
  optional `$headers` argument of `producev()` is omitted** — the naive call
  `producev($partition, 0, $payload)` gets no headers, while adding an
  explicit empty array `producev($partition, 0, $payload, null, [])` makes
  injection (incl. baggage) work. Verified empirically both ways.
  **Root cause (upstream-verified)**: the
  [rdkafka instrumentation](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtRdKafka/src/ExtRdKafkaInstrumentation.php)
  *does* unconditionally set `$params[4]` from its pre-hook — but the
  `ext-opentelemetry` extension silently drops arguments *added* to native
  functions, ever since argument expansion was disabled to prevent segfaults
  ([opentelemetry-php-instrumentation#84](https://github.com/open-telemetry/opentelemetry-php-instrumentation/pull/84),
  formalized in [#120](https://github.com/open-telemetry/opentelemetry-php-instrumentation/pull/120);
  original segfault:
  [opentelemetry-php#1155](https://github.com/open-telemetry/opentelemetry-php/issues/1155)).
  The extension README documents an escape hatch,
  `opentelemetry.allow_stack_extension=On` — and we **confirmed empirically**
  that with that ini set, the naive no-headers `producev` call injects
  everything including baggage (http→kafka and kafka→kafka flip to ✅; run
  archived at `results/experiments/otel-php-allow-stack-extension.json`).
  Neither the argument requirement nor the ini setting is mentioned in the
  [instrumentation README](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtRdKafka/README.md),
  and the drop produced no error_log line in our runs. No upstream issue
  exists for any of this — filing candidate. RabbitMQ: no php-amqplib
  instrumentation exists (only `ext-amqp`, a different client;
  no upstream feature request either) — all rabbit legs lose everything.
- **Elastic (elastic_apm 1.17.0)**: http→http only; Kafka and RabbitMQ are
  not in its supported-technologies list, and consumed messages start fresh
  traces on the outbound HTTP leg.

## 8. Elastic APM: one perfect agent, six partial ones

The headline: **Elastic's Java agent (1.56.0) propagates `traceparent`,
`tracestate`, and W3C `baggage` across all 16 boundaries** — the only
non-OpenTelemetry agent in the matrix to do so, and on messaging hops it adds
its binary `elasticapmtraceparent` header alongside the W3C set. Elastic's
[agent spec](https://github.com/elastic/apm/blob/main/specs/agents/tracing-distributed-tracing.md)
mandates baggage support; only Java delivers it end-to-end.

Everywhere else, coverage collapses by language:

- **Go (apm-agent-go v2.7.12)**: `apmhttp`/`apmgrpc` wrappers plus the
  documented manual traceparent/tracestate pattern carry *trace identity*
  across all 16 boundaries, but the agent has **no baggage API at all** —
  explicitly declined upstream
  ([apm-agent-go#537](https://github.com/elastic/apm-agent-go/issues/537),
  closed not-planned: "no plans for baggage at the moment");
  [apmot/span.go](https://github.com/elastic/apm-agent-go/blob/main/module/apmot/span.go#L127-L136):
  "SetBaggageItem is a no-op; we do not support baggage." This contradicts
  Elastic's own agent spec ("Agents MUST parse, validate, and attach the
  `baggage` header"). Baggage absent on all 16.
- **Node (elastic-apm-node 4.18.0)**: traceparent + tracestate over
  HTTP/gRPC/kafkajs; **no W3C baggage implementation**; **no amqplib
  support** (absent from the
  [supported technologies](https://www.elastic.co/docs/reference/apm/agents/nodejs/supported-technologies);
  Elastic's docs direct users to manual traceparent passing for queues —
  amqplib requests
  [#1188](https://github.com/elastic/apm-agent-nodejs/issues/1188) and
  [#1791](https://github.com/elastic/apm-agent-nodejs/issues/1791) have been
  open since 2019/2020) — all RabbitMQ legs empty.
- **Python (elastic-apm 6.26.2)**: HTTP-inbound works; gRPC-inbound continues
  the trace but **drops foreign `tracestate` entries**, and we captured an
  outright bug: the outbound `tracestate` header emitted after a gRPC hop is
  the literal string `tracestate` (the header *name* as its value). **Root
  cause found in source** — a one-line bug in
  [`server_interceptor.py`](https://github.com/elastic/apm-agent-python/blob/v6.26.2/elasticapm/contrib/grpc/server_interceptor.py#L66-L73):
  `elif metadata.key == "tracestate": tracestate = metadata.key` (the key
  assigned instead of the value), unfixed on main and unreported upstream
  (filing candidate; the async gRPC server interceptor is a second bug in
  the same report — it never reads `invocation_metadata` at all). No Kafka
  (confluent-kafka) or RabbitMQ (pika) support — pika requested in
  [apm-agent-python#678](https://github.com/elastic/apm-agent-python/issues/678),
  open since 2019.
- **.NET (1.34.7, startup-hook zero-code)**: HTTP + gRPC perfect incl.
  baggage. The agent's own Kafka/RabbitMQ instrumentation exists **only in
  the CLR-profiler deployment** (x64-only on Windows and Linux — it cannot
  run on arm64 at all), and even the profiler caps at `RabbitMQ.Client
  <7.0.0` and `Confluent.Kafka <3.0.0`
  ([profiler docs](https://www.elastic.co/docs/reference/apm/agents/dotnet/setup-auto-instrumentation):
  Kafka and RabbitMQ are the only technology rows with no startup-hook
  marker); the startup hook covers neither. Measured nuance: **RabbitMQ
  trace context still flows** (`*→rmq` inject and `rmq→*` continuation both
  ✅ for traceparent/tracestate) because RabbitMQ.Client 7.x ships *native*
  `ActivitySource` instrumentation that the elastic agent's OpenTelemetry
  bridge picks up — but baggage does not ride along (see §4's RabbitMQ
  root cause: the client's default extractor carries trace id/state only).
  Kafka (Confluent.Kafka has no native ActivitySource) measures fully
  absent: `*→kafka` no inject, `kafka→*` new trace.
- **Ruby (elastic-apm 4.9.0)**: http→http only. gRPC support exists but is
  *experimental and manual* (hand-added
  `ElasticAPM::GRPC::ClientInterceptor` at stub creation — no zero-code
  path); Kafka is covered only via the Racecar framework, and even there the
  [racecar spy](https://github.com/elastic/apm-agent-ruby/blob/main/lib/elastic_apm/spies/racecar.rb)
  never reads message headers (`start_transaction` with no `trace_parent`),
  so consume-side continuation is impossible even in the supported
  framework. No bunny or client-level Kafka instrumentation, and no upstream
  requests for them exist.
- **PHP**: see §7.

Practical summary: outside Java (and HTTP-only stacks on .NET/Node), Elastic
APM cannot currently carry application baggage across protocol boundaries.

## 9. New Relic: no baggage, ever — and seven agents that disagree with each other

Run with a real free-tier license key: New Relic's trace identity is
account-scoped (the `tracestate` entry is
`<trustedAccountKey>@nr=0-0-<accountId>-<appId>-…` and the vendor `newrelic`
header embeds the same ids), and agents only learn those values from the
collector connect handshake. No connect, no headers — verified three ways in
`results/nokey/`: the Node agent without a key halts (yet its require-time
hooks still half-propagate bare W3C on gRPC/AMQP-inbound paths — an
accident, not a mode), and the Go agent with `ConfigEnabled(false)` (the
documented dev/CI answer in
[newrelic/go-agent#46](https://github.com/newrelic/go-agent/issues/46)) is a
total no-op: zero headers on all 16 boundaries, propagation included.
Because several agents also *connect lazily* on first traffic (Python, PHP),
`run.sh` primes each newrelic variant with a discarded driver pass and
measures steady state — the unprimed Python run scored 4/16 instead of its
real 16/16.

**Baggage: zero, everywhere.** No New Relic agent carries W3C `baggage` or
`ot-baggage-*` on any boundary — the concept does not exist in their DT
model, and the headers are not forwarded. If you route on baggage, New Relic
contributes nothing on any protocol.

**Trace context: every agent has a different shape** (W3C boundaries out of
16, all zero-code except Go):

- **Python (13.5.0): 16/16** — flask, requests, grpcio, confluent-kafka and
  pika are all instrumented in both directions; the only perfect NR agent.
  Emits W3C only (no vendor `newrelic` header): v13.0.0 flipped
  `exclude_newrelic_header` to true by default
  ([PR #1721](https://github.com/newrelic/newrelic-python-agent/pull/1721),
  May 2026).
- **Ruby (10.7.1): 12/16 W3C, 16/16 vendor** — everything works, but bunny
  *publish* injects only the legacy `newrelic` header: on all four `*→rmq`
  boundaries `traceparent`/`tracestate` are absent, so a W3C-only consumer
  downstream (or another vendor's agent) sees nothing.
- **Go (v3.44.2): 12/16** — manual wiring per vendor docs (nrgrpc, nrsarama
  with explicit `AcceptDistributedTraceHeaders` on consume, nramqp). All
  Kafka-*outbound* legs are dead, and the root cause is a **visible bug in
  nrsarama's producer**
  ([producer.go](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/producer.go)):
  it builds a carrier whose `Set` would copy headers into `msg.Headers`, but
  then calls `InsertDistributedTraceHeaders(carrier.Header)` on the plain
  embedded `http.Header` — bypassing `Set`, so the DT headers land in a
  throwaway map and never reach the Kafka message. The consumer side
  ([consumer.go](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/consumer.go))
  never calls `AcceptDistributedTraceHeaders` either (it even calls
  *Insert* on the consumed headers), and neither does the official example —
  our relay's manual accept went beyond the documented flow. Neither bug is
  reported upstream — filing candidates.
- **Node (14.3.10): 9/16** — HTTP/gRPC/RabbitMQ full in both directions;
  Kafka dead in both directions because **kafkajs instrumentation is gated
  behind a default-off feature flag**
  ([`kafkajs_instrumentation: false`](https://github.com/newrelic/node-newrelic/blob/v14.3.10/lib/feature_flags.js)
  since [v11.19.0](https://github.com/newrelic/node-newrelic/releases/tag/v11.19.0),
  Jun 2024, never promoted): with the flag off the agent skips kafkajs
  entirely. **Empirically confirmed**: rerunning with
  `NEW_RELIC_FEATURE_FLAG_KAFKAJS_INSTRUMENTATION=true` flips all seven
  Kafka boundaries to ✅ (W3C in both directions) — 16/16, one env var
  apart (run archived at
  `results/experiments/newrelic-node-kafkajs-flag.json`). Emits W3C only:
  v14.0.0 flipped `exclude_newrelic_header` to true by default
  ([PR #3980](https://github.com/newrelic/node-newrelic/pull/3980) — note
  New Relic's config docs still list the old default).
- **Java (9.4.0): 6/16** — HTTP/gRPC-inbound → HTTP/gRPC/RabbitMQ-outbound
  only. kafka-clients instrumentation is telemetry-only *by default*: Kafka
  DT header injection is opt-in via
  `class_transformer: com.newrelic.instrumentation.kafka-clients-spans-0.11.0.0: enabled: true`
  ([docs](https://docs.newrelic.com/docs/apm/agents/java-agent/instrumentation/java-agent-instrument-kafka-message-queues/)
  — New Relic warns it adds 150–200 bytes per message). Untested here;
  backlog as a `newrelic-java-kafkaspans` variant. spring-rabbit publish
  injects but the listener never accepts — **mechanism found in source**:
  the agent ships no spring-rabbit module at all (only
  [rabbit-amqp-2.7.0/5.0.0](https://github.com/newrelic/newrelic-java-agent/blob/main/instrumentation/rabbit-amqp-5.0.0/src/main/java/com/rabbitmq/client/Consumer_Instrumentation.java)
  and spring-kafka); DT accept is woven around `Consumer.handleDelivery` on
  the AMQP connection thread, but spring-rabbit's listener container
  processes the message on a different thread, outside that transaction.
  The Kafka consumer never accepts either — every messaging-inbound row is
  dead. No upstream issue covers either gap.
- **.NET (10.54.0): 6/16** — the mirror image of Java on the produce side:
  Confluent.Kafka *is* instrumented (all `*→kafka` legs inject) but the
  `*→rmq` legs are dead: the agent instruments the classic
  `IModel.BasicPublish` surface, which RabbitMQ.Client 7 removed entirely,
  while this relay uses 7.2.2's async-only `IChannel.BasicPublishAsync` —
  the v7 async instrumentation PR
  ([newrelic-dotnet-agent#2890](https://github.com/newrelic/newrelic-dotnet-agent/pull/2890))
  was closed on a ValueTask blocker. The compat docs' "7.1.2 verified" claim
  refers to the **hybrid agent** path
  ([#2885](https://github.com/newrelic/newrelic-dotnet-agent/issues/2885):
  "RabbitMQ 7+ versions are supported with the new hybrid agent feature in
  agent versions 10.49.0+"), which picks up the client's native
  ActivitySource — but it is
  [disabled by default](https://docs.newrelic.com/docs/apm/agents/manage-apm-agents/opentelemetry-api-support/)
  and produces span links, not parent/child. Default config: dead, exactly
  as measured. Messaging-inbound all dead.
- **PHP (12.10.0.39): 2/16** — `http→http` (curl) and `http→rmq`
  (php-amqplib ≥ 3.7 publish instrumentation, agent ≥ 11.6) carry all three
  headers; rdkafka and ext-grpc are unsupported; the long-running CLI
  consumers never accept inbound context. Docker footgun: `newrelic-install`
  *symlinks* the extension into the extracted tarball directory by default —
  deleting the tarball leaves a dangling symlink and a silently
  uninstrumented app; build with `NR_INSTALL_USE_CP_NOT_LN=1`.

**`tracestate` hygiene is perfect**: every NR agent that continued a trace
appended its `@nr` entry and preserved our foreign `sdtest=v1` entry — no
corruption in 112 delivered tests.

## 10. Rust: manual-by-design, and both tools ace it

No auto-instrumentation exists for Rust; the relay uses the documented
carrier/propagator APIs (opentelemetry-rust 0.32 + opentelemetry-http;
carriers over tonic `MetadataMap`, rdkafka headers, lapin `FieldTable`).

- **otel-rust**: all 16 boundaries, all headers.
- **datadog-rust** ([datadog-opentelemetry](https://docs.datadoghq.com/tracing/trace_collection/automatic_instrumentation/dd_libraries/rust/)
  0.5.2, "Preview"): all 16 boundaries — `x-datadog-*`, traceparent,
  tracestate, and W3C baggage. Notable: Datadog's docs footnote claims baggage
  is unsupported in Rust, but 0.5.2's source implements it (delegating to the
  OTel `BaggagePropagator`) and enables it by default — the docs lag the code.
  No `ot-baggage-*` style exists in this SDK.

The catch: none of it happens unless the developer wires the carriers at
every hop — "supported" here means "the API exists", not "your service is
covered by attaching an agent".

## 11. Go build-time weaving: zero source changes, two very different halves

Go has no runtime attach, but both vendors now ship compile-time weavers, and
the matrix measures them as first-class variants. Both compile the *same kind
of deliberately naive app* (single `main.go`, zero tracing imports — see
`relays/go-orchestrion/` and `relays/go-otelc/`); the only difference from an
uninstrumented build is the build command.

**otel-go-compiletime** — [OpenTelemetry Go Compile-Time
Instrumentation](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation)
("otelc", v1.1.0, stable since v1.0; weaves OTel SDK v1.45.0). Installed as a
Go 1.24+ tool dependency; the build is `go tool otelc go build`. With no
committed config it analyzes the dependency graph and enables every
instrumentation it has for libraries the app imports (here: net/http client +
server, grpc client + server, segmentio/kafka-go producer + consumer, plus
the SDK bootstrap that honors `OTEL_PROPAGATORS`).

- **HTTP/gRPC-inbound legs (8/16): everything propagates** — traceparent,
  foreign tracestate, and W3C baggage — *including into Kafka*. kafka-go's
  `Writer.WriteMessages(ctx, …)` takes a context, and the woven producer
  injects from it. A naive app gets baggage across an async produce hop with
  zero source changes: no runtime-attach agent in any language does better
  on those legs.
- **Kafka-inbound legs (4/16): total loss.** kafka-go's `Reader` is a
  pull-style API (`ReadMessage(ctx)` in an app-owned loop) with no callback
  boundary, so the woven consumer instrumentation has nowhere to hand the
  extracted context back to application code — the app's loop context is
  `context.Background()` and outbound calls carry nothing (not even a new
  trace: with no valid span context, the W3C propagator injects nothing).
  The dominant failure class of the whole matrix (§14.1), reproduced by a
  compile-time weaver.
- **RabbitMQ legs: no instrumentation exists.** otelc's supported list has
  no AMQP client, so all rabbitmq cells are dark — same coverage-allowlist
  economics as every runtime agent (§14.3). (True of otelc 1.1.0 and earlier.
  otelc 1.2.0 instruments amqp091-go: `http->rabbitmq` and `grpc->rabbitmq`
  now pass, and `rabbitmq->*` starts a new trace. The annotation
  `go-weavers-no-amqp-instrumentation` has the current behaviour.)

**datadog-go-orchestrion** —
[Orchestrion](https://github.com/DataDog/orchestrion) v1.13.0 weaving
dd-trace-go v2.10.1. Setup is the documented `orchestrion pin` (which writes
`orchestrion.tool.go` importing the catch-all
`dd-trace-go/orchestrion/all/v2`), build is `orchestrion go build`. Same
Kafka client and tracer version as the hand-wired datadog-go relay
(IBM/sarama v1.60.2), so the two rows isolate exactly what weaving does and
does not replace.

- **http/grpc → http/grpc (4/16): everything propagates** — traceparent,
  tracestate, W3C baggage, *and* `ot-baggage-*`. Orchestrion's woven
  dd-trace-go bridges the inbound request context to outbound clients (via
  its goroutine-local context machinery), so these legs match the hand-wired
  relay exactly.
- **Every Kafka-touching leg: headers present, context lost (`new_trace`).**
  On produce, sarama's `SyncProducer.SendMessage(msg)` takes no context at
  all; the woven producer starts a *root* produce span and injects that, so
  `x-datadog-*`/`traceparent` arrive carrying a brand-new trace-id and the
  seeded baggage/tracestate entries are gone. On consume, the woven
  consumer-group instrumentation does not continue the trace seeded in the
  record headers either — outbound calls again carry a fresh trace. This is
  the span-centric failure shape (§14.2) in a new costume: dashboards show
  spans everywhere, but continuity and baggage died at the broker. The
  hand-wired relay passes these same legs with the same library versions,
  because the dd contrib README's manual pattern (inject from the request
  context before `SendMessage`, extract-and-activate in the handler) does
  the two steps the weaver skips.
- **RabbitMQ legs: dark** — dd-trace-go has no amqp091 integration for
  Orchestrion to enable (same gap as the hand-wired row, §6 economics).

Two operational notes, both learned the hard way. First, **woven
dependencies are invisible to Go's build metadata**: Orchestrion links
dd-trace-go into the binary behind the go tool's back, so `go version -m`
shows *no trace of the tracer* — verifying weaving requires symbol
inspection (`go tool nm | grep dd-trace-go`), and both Dockerfiles assert
this so an un-woven binary fails the image build rather than silently
publishing a dark row. Second, `orchestrion pin` prints a spurious
`removing unnecessary import … replacement directory … does not exist`
warning against the published `all/v2` module; it is cosmetic (the build
weaves regardless — verified by symbols), but it cost this project an
afternoon, and a real team could easily conclude the tool is broken.

## 12. Baseline (no instrumentation): seven clean zeros — and .NET

Control runs: every language's relay with no agent, wrapper, or tracing gem
attached (`none-*` variants, 128 hops). Seven of eight languages behave as
expected — nothing propagates on any of the 16 boundaries, confirming that
everything in the instrumented tables is attributable to the tool under
test, not to the apps or the harness.

The exception is .NET. With **no agent attached**, ASP.NET Core + HttpClient
propagate on all four http/grpc → http/grpc boundaries:

- `traceparent`: same trace-id, fresh span-id — the runtime genuinely
  participates in the trace. ASP.NET Core hosting creates an `Activity` per
  request, and `HttpClient`'s DiagnosticsHandler injects it on the way out
  (gRPC inherits this because `Grpc.Net.Client` rides on HttpClient).
- `tracestate`: preserved verbatim.
- W3C `baggage` in → **`Correlation-Context` out**. The default
  `DistributedContextPropagator` on .NET 8/9 is the pre-W3C "legacy"
  propagator: it reads the standard `baggage` header into `Activity.Baggage`
  but re-emits it under the obsolete draft name, so values survive one hop
  and are then invisible to any W3C-only reader downstream.

Nothing propagates on Kafka/RabbitMQ legs (no built-in messaging
propagation). Note the version dependence: [.NET 10 switches the default to
a strict W3C propagator](https://learn.microsoft.com/en-us/dotnet/core/compatibility/core-libraries/10.0/default-trace-context-propagator)
(`baggage` header out, strict encoding, `CreatePreW3CPropagator()` to opt
back) — an *uninstrumented* .NET service changes its propagation behavior on
a runtime upgrade. Harness runs used .NET 8.

## 13. Positive results worth stating

- **All 16 boundaries, all headers**: OTel Java agent 2.31.1, OTel Node
  auto-instrumentations 0.80.0, OTel Go (otelhttp/otelgrpc/kotel + documented
  propagator plumbing), otel-rust 0.32, dd-trace-js 6.14.0, dd-trace-go
  v2.10.1, dd-trace-dotnet 3.52.0, datadog-rust 0.5.2, and (trace context +
  baggage) Elastic Java 1.56.0.
- **New Relic Go** ships an official amqp091 integration
  ([nramqp](https://pkg.go.dev/github.com/newrelic/go-agent/v3/integrations/nramqp))
  that both inserts and accepts DT headers per delivery — unlike its Kafka
  contrib, no manual accept step is needed (verified: all 8 RabbitMQ legs
  pass, §9).
- **New Relic Python (13.5.0)** is a quiet star: 16/16 boundaries with
  zero code — the only New Relic agent, and one of very few agents of any
  vendor, to activate consumed context on both Kafka and RabbitMQ (§9).
- **OTel Go compile-time weaving carries baggage into Kafka with zero source
  changes** (otelc, §11): traceparent + tracestate + baggage all survive
  http/grpc → kafka on a naive app whose build command is the only thing
  that changed. That is a propagation result no runtime-attach agent can
  reach in Go, because none exists.
- **tracestate foreign-entry preservation** was honored by every tracer that
  continued a trace — including all seven New Relic agents, which append
  their `@nr` entry alongside foreign entries — with one exception:
  elastic-apm Python's gRPC path (§8).
- **Canary**: `x-sd-canary` never leaked in any of the 752 recorded tests
  (624 instrumented + 128 baseline) — relays re-emit, not proxy.

## 14. Cross-cutting conclusions

1. **The dominant failure class is consume-side context activation.** Across
   all four vendors, instrumentations inject correctly but fail to make the
   extracted context current for user code: OTel Python (confluent-kafka),
   OTel .NET (Kafka links; RabbitMQ baggage), OTel Ruby (bunny), dd-py
   (kafka), dd-php (rdkafka), NR Java (kafka + spring-rabbit listeners),
   NR .NET (kafka + rabbitmq), NR Node (kafkajs), NR PHP (both workers).
   Injection is table stakes; the consumer is where propagation actually
   dies.
2. **Span-centric inject sites** (dd-java, #11286) lose exactly the data that
   lives on context and not on spans — baggage — while trace identity
   survives, making the failure invisible to trace-waterfall debugging.
3. **Coverage is an allowlist, not a protocol feature.** dd-py/pika,
   dd-rb/bunny, otel-php/amqplib, elastic everywhere outside Java: the
   canonical client library simply isn't on the vendor's list, and the
   result is silent, total loss.
4. **Optional-argument traps exist**: OTel PHP injects into Kafka only when
   `producev()` is called with the `$headers` argument present (§7) — the
   difference between ✅ and ❌ is one `[]` in application code, or one
   undocumented ini line (`opentelemetry.allow_stack_extension=On`,
   empirically confirmed to fix the naive call; see §7 and REFERENCES.md).
5. W3C Baggage being HTTP-scoped and still a Candidate Recommendation
   ([spec](https://www.w3.org/TR/baggage/), snapshot 2024-05-30) gives
   vendors cover for these gaps — the spec's silence on non-HTTP carriers is
   where implementations diverge.
6. The OpenTracing-era `ot-baggage-*` headers outlive their deprecation for a
   reason: in dd-trace-java they cross all sixteen boundaries while the
   modern `baggage` header crosses one.
7. **A vendor is not a behavior.** New Relic's seven agents produce seven
   different coverage shapes from identical applications (16/16 down to
   2/16), disagree on which protocols inject (Kafka-out: .NET/Ruby/Python
   only; RabbitMQ-out: everyone *except* .NET), and even on which headers to
   send (Node/Python emit W3C only; Ruby's bunny publish emits the vendor
   header only). "We support distributed tracing" tells you nothing about a
   specific language × protocol cell.
8. **Propagation can be backend-gated.** Unlike OTel/Datadog/Elastic, whose
   propagation works with no backend at all, New Relic agents create no
   headers until their collector handshake supplies account identity — and
   `Enabled: false` (the documented dev/CI switch) kills propagation
   entirely (§9). A New Relic outage or misconfigured key silently degrades
   *propagation*, not just telemetry.
9. **Several failures are visible, unreported bugs in vendor source.** Three
   of our root causes are readable in the shipped code and have no upstream
   issue: Elastic Python's `tracestate = metadata.key` one-liner (§8),
   nrsarama's producer injecting DT headers into a discarded map (§9), and
   dd-trace-php closing the consume span before user code runs (§7). Add
   the four coverage gaps nobody has requested (dd-rb bunny, OTel PHP
   php-amqplib, OTel Ruby gRPC server, OTel PHP producev trap) and a clear
   picture emerges: these paths are not merely broken — nobody is measuring
   them. See REFERENCES.md for the full filing list.
10. **One flag away.** Three separate cells flipped from fully dark to fully
   green with a single configuration line, none of them documented where
   users would look: OTel PHP's `opentelemetry.allow_stack_extension=On`
   (§7), New Relic Node's `NEW_RELIC_FEATURE_FLAG_KAFKAJS_INSTRUMENTATION=true`
   (§9), and dd-py's `DD_KAFKA_PROPAGATION_ENABLED=true` (§2, documented but
   default-off). Empirical testing of *your* config is the only way to know
   which side of the flag you're on.
11. **Build-time weaving moves the boundary, not the failure class.** Go's
   compile-time weavers (§11) deliver genuine zero-source-change propagation
   on synchronous legs — otelc even carries baggage into Kafka — but both
   tools break exactly where the runtime agents break: at the consume side
   of async hops, and wherever the client library isn't on the
   instrumentation allowlist. The API shape decides the ceiling (kafka-go's
   context-accepting producer propagates; sarama's context-free
   `SendMessage` cannot), and a weaver can only work with the shape it is
   given.
