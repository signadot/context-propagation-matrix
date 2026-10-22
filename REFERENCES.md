# Upstream references — evidence for every non-success case

Companion to [`FINDINGS.md`](FINDINGS.md): each failing (or surprising) cell
family in the published matrix (`MATRIX.md` on the
[latest release](https://github.com/signadot/context-propagation-matrix/releases/latest))
mapped to upstream
evidence — GitHub issues, PRs, normative specs, and vendor docs. Every link
below was fetched and verified against its live content on 2026-09-06;
quotes are verbatim. Where **no upstream source exists**, that is recorded
explicitly — those are candidates for us to file, and are findings in their
own right.

Format per entry: **source** (type, state) — relevance, with a short quote
where it earns its place.

---

## OpenTelemetry

### Python: Kafka-inbound loses everything (`kafka→*` new trace, baggage lost)

Mechanism confirmed in source: `_create_new_consume_span` in the
confluent-kafka instrumentation calls `start_span(..., links=links)` and the
package never calls `opentelemetry.context.attach()` — the producer context
becomes a span *link* on a span that is never made current.

- [opentelemetry-python-contrib#1674](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1674)
  (issue, **open** since Feb 2023) — exact match: the `poll()` wrapper
  creates the consume span without using the extracted context.
  > "the span that's started after record is received will use the current
  > span's context even if the links contain the context from the message
  > headers"
- [opentelemetry-python-contrib#1962](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1962)
  (issue, open) — maintainer-authored meta-issue on the instrumentation's
  problems; item 5: "Not correct extraction of remote context for Consumer".
- [opentelemetry-python-contrib#1732](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1732)
  (issue, closed as duplicate) — same symptom in the sibling *kafka-python*
  instrumentation ("The actual traceId is 0 … kafka consumer headers
  traceparent exists"), closed as dup of
  [#3026](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3026)
  (issue, closed) — the *aiokafka* variant, which asks for exactly the
  missing call: "Would it be an idea to do
  `opentelemetry.context.attach(extracted_context)`…?"
- [opentelemetry-python-contrib#3465](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3465)
  (issue, closed) — the maintainer position on the whole class:
  > "I think the instrumentation is right, if you don't want the artificial
  > span, pushing the context should be enough as the example below."
- [opentelemetry-python-contrib PR #1678](https://github.com/open-telemetry/opentelemetry-python-contrib/pull/1678)
  (PR, closed **unmerged**, stale-bot) — the only fix attempt for #1674,
  dead since tests went red.

Summary: reported at least four times across confluent-kafka, kafka-python,
and aiokafka since 2023; maintainers treat manual `context.attach` as
intended usage; no merged fix and no open PR.

**Not found upstream**: no issue names the *baggage* loss specifically (all
frame it as trace-id/parenting loss) — candidate to file or add to #1674.

### .NET: Kafka consumers link instead of continue (`kafka→*` new trace, baggage lost)

A deliberate, spec-driven decision in the auto-instrumentation we test:

- [opentelemetry-dotnet-instrumentation#3434](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/issues/3434)
  (issue, closed — implemented) — titled verbatim: "[kafka] do not use the
  message creation context as a parent for receive spans by default".
- [opentelemetry-dotnet-instrumentation PR #3438](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/pull/3438)
  (PR, **merged**, shipped in 1.7.0) — CHANGELOG: "Do not use message
  creation context as a parent for consumer spans for `Confluent.Kafka`
  client instrumentation."
- [OTel messaging semantic conventions](https://github.com/open-telemetry/semantic-conventions/blob/main/docs/messaging/messaging-spans.md)
  (spec) — the normative rule:
  > "These conventions use spans links as the default mechanism to correlate
  > producers and consumer(s) because … It is the only option to correlate
  > producer and consumer(s) in batch scenarios as a span can only have a
  > single parent."
- [OTEP-0220](https://github.com/open-telemetry/opentelemetry-specification/blob/main/oteps/trace/0220-messaging-semantic-conventions-span-structure.md)
  (spec) — origin of the links-by-default rule
  (via [semantic-conventions#198](https://github.com/open-telemetry/semantic-conventions/issues/198));
  parent-child later re-allowed as an *option* by
  [semantic-conventions PR #1414](https://github.com/open-telemetry/semantic-conventions/pull/1414)
  (merged Sep 2024).
- [opentelemetry-dotnet-contrib#3995](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/3995)
  (issue, **open**) — same symptom in the contrib package; maintainer: "I
  think that there is no bug here. See … semantic-convention 1.24.0. As long
  as we are not switching to the modern version there is nothing to change."
- [opentelemetry-dotnet-contrib#2351](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/2351)
  (issue, closed by-design) — `Activity.Current` null after `Consume`;
  component owner: "This is be design according to the otel messaging specs."

**Not found upstream**: no issue anywhere about *baggage* dropped across
Kafka in OTel .NET — candidate for us to file.

### .NET: RabbitMQ continues the trace but drops baggage (`rmq→*` trace ✅, baggage ❌)

The propagation here is RabbitMQ.Client's own, not OTel's — and its default
path physically cannot carry baggage:

- [rabbitmq-dotnet-client PR #1261](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1261)
  (PR, merged; RabbitMQ.Client 7.0) — the native self-instrumentation:
  "This implements an ActivitySource that creates and propagates Activities
  for BasicPublish (send), BasicDeliver (receive)…" (design chosen in
  [#776](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/776)).
  Its `DefaultContextExtractor` calls `ExtractTraceIdAndState(...)` only —
  the word "baggage" does not appear in the propagation code.
- [rabbitmq-dotnet-client PR #1528](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1528)
  (PR, merged) — the smoking gun: concedes default propagation can't do
  baggage and adds an opt-in package instead.
  > "Making it possible to override the default context propagation in
  > RabbitMQActivitySource to make it possible to have proper OpenTelemetry
  > Baggage propagation; Adding a separate OpenTelemetry integration
  > package…"
- [rabbitmq-dotnet-client#1728](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/1728)
  (issue, **open** since Nov 2024) — "Ship `RabbitMQ.Client.OpenTelemetry`
  1.0.0": the baggage-capable helper has only ever shipped as
  1.0.0-rc.1/rc.2 on NuGet; zero-code deployments never wire it in.
- [opentelemetry-dotnet-instrumentation#3662](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/issues/3662)
  (issue, **open**) — OTel side tracks the gap as an unbuilt feature: "add
  baggage support for `deliver` spans".
- [rabbitmq-dotnet-client PR #1779](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1779)
  (PR, merged, shipped v7.2.0; from
  [#1666](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/1666)) —
  version caveat for the ✅ half of this finding: consume-side parent-child
  continuity (`UsePublisherAsParent = true`) is only the default since
  **7.2.0**; on 7.0–7.1 the deliver activity was link-only. Our relay pins
  7.2.2.
- [rabbitmq-dotnet-client#1967](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/1967)
  (issue, open) — "The OpenTelemetry / System.Diagnostics.Activity tracing
  implementation has never had a systematic review" — the surface is still
  settling.

**Not found upstream**: no request for baggage in the *default*
extractor/injector path of rabbitmq-dotnet-client (only the opt-in package
route of PR #1528).

### Ruby: gRPC-inbound starts a new trace (client-only instrumentation)

- [opentelemetry-ruby-contrib gRPC README](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/grpc/README.md)
  (docs) — the official statement of scope:
  > "[!WARNING] Right now, the gem only instruments outbound requests to
  > gRPC services"
- [opentelemetry-ruby-contrib#10](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/10)
  (issue, closed) — the original 2020 request ("Convert Datadog
  instrumentation for gRPC to OpenTelemetry"), unfilled for ~4 years until a
  community gem ([hibachrach/opentelemetry-instrumentation-grpc](https://github.com/hibachrach/opentelemetry-instrumentation-grpc),
  client-only, announced in
  [#1041](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/1041))
  was donated via
  [PR #1164](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1164)
  (merged Sep 2024).
- [opentelemetry-ruby-contrib gruf README](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/gruf/README.md)
  (docs) — server-side gRPC spans exist only for the Gruf framework, not the
  plain `grpc` gem.

Nuance for accuracy: `opentelemetry-instrumentation-all` *does* now bundle
the gRPC gem (`~> 0.5.0`, verified in the gemspec) — the correct claim is
"loaded but client-only", not "absent". Client injection additionally
requires wiring the interceptor into stubs, which zero-code setups don't do.

**Not found upstream**: no issue in contrib or the community repo requests
*server-side* instrumentation of the plain `grpc` gem — candidate to file.

### Ruby: bunny consume drops all context (`rmq→*` new trace)

- [opentelemetry-ruby-contrib#526](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/526)
  (issue, **open** since Jun 2023) — exact match for our observation:
  > "Instead of propagating any contexts (including baggage), the bunny
  > instrumentation drops all contexts in the consumer and starts a new
  > trace."
- [opentelemetry-ruby discussion #1098](https://github.com/open-telemetry/opentelemetry-ruby/discussions/1098)
  (discussion, Jan 2022) — maintainer confirms links-by-design:
  > "The Bunny instrumentation adheres to the OTel messaging specification
  > and uses Links instead of Child Spans in consumers… Links lead to new
  > traces."
- [bunny patch_helpers.rb](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/bunny/lib/opentelemetry/instrumentation/bunny/patch_helpers.rb)
  (source) — the mechanism: `with_process_span` parents on the
  instrumentation's own `:tracer_receive_headers` (a local receive span it
  injected itself); the producer's real context becomes only a `Link`.
- [opentelemetry-ruby PR #566](https://github.com/open-telemetry/opentelemetry-ruby/pull/566)
  (PR, merged 2021) — design lineage: "Basic instrumentation for bunny based
  on the existing ruby_kafka instrumentation."

No merged PR fixes consume-side behavior; #526 has been open 3+ years.

### PHP: no gRPC in either direction

- [grpc.io PHP quickstart](https://grpc.io/docs/languages/php/quickstart/)
  (docs) — "You can only create gRPC clients in PHP. Use another language to
  create a gRPC server."
- [grpc/grpc#38481](https://github.com/grpc/grpc/issues/38481) (issue,
  **open**, filed by a Google engineer) — "gRPC does not support a PHP
  server implementation. However, implementing a gRPC server in PHP is
  possible using RoadRunner."
- [opentelemetry-php-contrib instrumentation tree](https://github.com/open-telemetry/opentelemetry-php-contrib/tree/main/src/Instrumentation)
  (source) — the full list of 30 auto-instrumentations contains no gRPC
  entry; gRPC exists in the OTel PHP ecosystem only as the OTLP *export
  transport*, never as instrumentation of application `ext-grpc` calls.
- [dd-trace-php#805](https://github.com/DataDog/dd-trace-php/issues/805)
  (issue) — Datadog's guidance for RoadRunner gRPC is manual
  `dd_trace_method` hooks.

**Not found upstream**: no issue in any OTel PHP repo proposes instrumenting
`ext-grpc` client calls — candidate to file.

### PHP: rdkafka `producev()` injects only with an explicit `$headers` argument

Root-cause chain, fully verified — the instrumentation *tries* to add the
argument; the extension layer silently drops it:

- [ExtRdKafkaInstrumentation.php](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtRdKafka/src/ExtRdKafkaInstrumentation.php)
  (source) — the pre-hook unconditionally sets the 5th parameter slot:
  `$params[4] = isset($params[4]) ? array_merge($params[4], $carrier) : $carrier`.
- [opentelemetry-php-instrumentation PR #84](https://github.com/open-telemetry/opentelemetry-php-instrumentation/pull/84)
  (PR, merged Aug 2023) — why that's a no-op for native methods like
  `producev`:
  > "expanding arguments for internal functions from a pre callback causes a
  > segfault. until we can fix it properly, disable the behaviour and log to
  > error_log."
- [opentelemetry-php#1155](https://github.com/open-telemetry/opentelemetry-php/issues/1155)
  (issue, closed) — the original segfault, discovered while writing
  messaging instrumentation ("hooking into `AMQPExchange::publish` and
  expanding the args there").
- [opentelemetry-php-instrumentation PR #120](https://github.com/open-telemetry/opentelemetry-php-instrumentation/pull/120)
  (PR, merged Jan 2024) — formalized the limit: parameters beyond
  `max(defined_param_count, callsite_param_count)` are dropped.
- [ext-opentelemetry README](https://github.com/open-telemetry/opentelemetry-php-instrumentation/blob/main/README.md)
  (docs) — the escape hatch, documented only in the extension repo:
  > "For internal functions, adding arguments not provided at the callsite
  > always requires stack extension… can be enabled by setting
  > `opentelemetry.allow_stack_extension` ini value to `On`."
- [ExtRdKafka README](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtRdKafka/README.md)
  (docs) — claims producev injection with **no mention** of the
  argument-presence requirement (the doc gap), and
  [PR #284](https://github.com/open-telemetry/opentelemetry-php-contrib/pull/284)
  (the instrumentation's origin) shows no reviewer caught the interaction
  with PR #84.
- [php-rdkafka topic.stub.php](https://github.com/arnaud-lb/php-rdkafka/blob/6.x/topic.stub.php)
  (source) — confirms `$headers` is optional:
  `producev(int $partition, int $msgflags, ?string $payload = null, ?string $key = null, ?array $headers = null, …)`.

**Empirical confirmation of the escape hatch** (2026-09-06): with
`opentelemetry.allow_stack_extension=On` set, the naive no-headers
`producev($partition, 0, $payload)` call injects traceparent, tracestate,
*and* baggage — `http→kafka` and `kafka→kafka` flip from ❌ to ✅. Run
archived at `results/experiments/otel-php-allow-stack-extension.json`. The
error_log warning promised by PR #84 did not appear in our container logs.

**Not found upstream**: no issue anywhere mentions the producev
optional-argument no-op — filing candidate (against
`open-telemetry/opentelemetry-php` per their convention), citing the chain
above.

### PHP: RabbitMQ entirely uncovered by OTel (`*→rmq` and `rmq→*` dead)

- [opentelemetry-php-contrib instrumentation tree](https://github.com/open-telemetry/opentelemetry-php-contrib/tree/main/src/Instrumentation)
  (source) — no `PhpAmqpLib` instrumentation exists; the only AMQP coverage
  is [ExtAmqp](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtAmqp/README.md)
  (the PECL `ext-amqp` client — a different library), and even there
  consumers are manual by design.
- [opentelemetry-php#937](https://github.com/open-telemetry/opentelemetry-php/issues/937)
  (issue, closed) — why consumer hooks are hard in PHP: "You cannot hook an
  anonymous function, only methods" (consume callbacks are closures).

Contrast: the rdkafka *consume* hook extracts **and attaches**
(`Context::storage()->attach($context)` in the source above), which is why
our `kafka→http` cell is ✅ — the OTel PHP asymmetry is outbound-side, not
consume-side.

**Not found upstream**: no feature request for php-amqplib instrumentation —
filing candidate (php-amqplib is the dominant PHP AMQP client).

### Go compile-time weaving (otelc): produce-side perfect, consume-side dark, no RabbitMQ

Variant `otel-go-compiletime` (naive app, `go tool otelc go build`): all
http/grpc-inbound legs propagate everything including baggage into Kafka;
all kafka-inbound legs lose everything; rabbitmq legs uninstrumented.

- [opentelemetry-go-compile-instrumentation](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation)
  (repo; v1.0 stable, v1.1.0 measured) — the supported-libraries table in
  [getting-started.md](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/docs/getting-started.md)
  lists `segmentio/kafka-go` as the only messaging client: no sarama, no
  franz-go, no AMQP client — the rabbitmq column measures that absence.
- [opentelemetry-go-compile-instrumentation#695](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/695)
  (issue, open) — maintainer-acknowledged messaging-semconv workstream that
  names our exact consume-side gap ("a process-span story for the zero-code
  gap", kafka-go span-links fix) and plans IBM/sarama as the next client.
  Structural cause of the dark kafka-inbound legs: kafka-go's `Reader` is a
  pull API with no callback boundary, so the woven consumer has no place to
  hand the extracted context to application code, whose loop context is
  `context.Background()`.
- [opentelemetry-go-compile-instrumentation#1076](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1076)
  (PR, merged) — `(*kafka.Reader).FetchMessage` instrumentation, confirming
  the consume span is created per message; the missing half is activation,
  not extraction.

---

## Datadog

### Java: W3C baggage crosses only `http→http` (trace context crosses all 16)

- [dd-trace-java#11286](https://github.com/DataDog/dd-trace-java/issues/11286)
  (issue, open) — the canonical report (Kafka). Beyond the known "feature not
  bug" reply, a second maintainer comment confirms the injection scope:
  > "Kafka instrumentation is indeed dropping all context elements except
  > tracing at injection." (PerfectSlayer, Datadog)
- [dd-trace-java PR #11330](https://github.com/DataDog/dd-trace-java/pull/11330)
  (PR, closed **unmerged**, never reviewed) — community fix that documents
  the span-centric inject-site root cause exactly:
  > "The Kafka producer instrumentation passes the active `AgentSpan` to
  > `defaultPropagator().inject()`, but `AgentSpan.get()` delegates non-span
  > context keys (like baggage) to `Context.root()` instead of the
  > thread-local `Context.current()`."
- [dd-trace-java#11510](https://github.com/DataDog/dd-trace-java/issues/11510)
  (issue, closed — fixed in 1.65.0) — same failure class on HTTP itself
  (Spring WebClient/Reactor): "Trace headers … are injected correctly, but
  `baggage` is consistently missing."
- [dd-trace-java PR #12027](https://github.com/DataDog/dd-trace-java/pull/12027)
  (PR, merged Jul 2026) — Datadog's own fix language is the span-vs-context
  distinction, verbatim:
  > "The connect-span path carried only the active AgentSpan across the
  > subscription → I/O thread hand-off, dropping the rest of the Datadog
  > Context (including baggage). Capture the full Context instead."
  With [PR #12033](https://github.com/DataDog/dd-trace-java/pull/12033)
  ("Propagates the full context, including W3C baggage, over various async
  calls") — the context-centric migration covers async/HTTP paths only;
  messaging inject sites remain span-centric, consistent with our 1.65.1
  measurement.

**Not found upstream**: no issue exists for baggage loss on gRPC, RabbitMQ,
or JMS boundaries — #11286 (Kafka) is the only messaging report; our
gRPC/RabbitMQ measurements appear unreported. No open PR fixes the messaging
inject sites.

### Python: Kafka injection off by default; consumer never continues context

- [ddtrace Kafka integration docs](https://ddtrace.readthedocs.io/en/stable/integrations.html#kafka)
  (docs) — "`DD_KAFKA_PROPAGATION_ENABLED` … Default: 'False'".
- [dd-trace-py PR #7434](https://github.com/DataDog/dd-trace-py/pull/7434)
  (PR, merged Dec 2023) — origin of the flag and the default-off rationale:
  > "The behavior is hidden behind a feature flag and is disabled by default
  > to prevent any unexpected changes to users' kafka tracing"
- [dd-trace-py PR #7345](https://github.com/DataDog/dd-trace-py/pull/7345)
  (PR, merged Oct 2023) — a Datadog engineer's own statement of the
  consume-side gap:
  > "`kafka.consume` and `kafka.produce` spans are not connected on the
  > trace view because the context isn't used when the `kafka.consume` span
  > is created."
- [dd-trace-py#8967](https://github.com/DataDog/dd-trace-py/issues/8967)
  (issue, closed) — flag on, traces still detached; the reporter's
  workaround is the exact missing step: "manually instrumenting our code
  with `tracer.context_provider.activate(Propagator.extract(dict(msg.headers())))`".
- [dd-trace-py#17980](https://github.com/DataDog/dd-trace-py/issues/17980)
  (issue, **open**, May 2026) — activation semantics dispute: "The current
  implementation is activating the first trace from the first message in a
  given batch … In Java SDK, the trace is activated for each Kafka record."
- [dd-trace-py PR #18667](https://github.com/DataDog/dd-trace-py/pull/18667)
  (PR, open) and [PR #19858](https://github.com/DataDog/dd-trace-py/pull/19858)
  (PR, merged Aug 2026) — the links-instead-of-continuation migration,
  gated behind `propagation_as_span_links`: "We can't do that directly
  because we would break existing semantics." Directly explains our observed
  new-trace-with-links shape.

**Not found upstream**: nobody has reported the *baggage* dimension of the
consumer gap (existing issues cover trace identity only).

### Python: pika/RabbitMQ not instrumented at all

- [Datadog Python compatibility docs](https://docs.datadoghq.com/tracing/trace_collection/compatibility/python/)
  (docs) — pika has zero mentions; the only AMQP row is Kombu (and Kombu
  isn't even auto-patched, per the
  [integration docs](https://ddtrace.readthedocs.io/en/stable/integrations.html)).
- [dd-trace-py#2047](https://github.com/DataDog/dd-trace-py/issues/2047)
  (issue, closed; feature request from 2021) — Datadog's disposition:
  > "The request for a pika integration is in our team's feature backlog and
  > is not planned for the short term." (emmettbutler, Datadog)
- [dd-trace-py#17225](https://github.com/DataDog/dd-trace-py/issues/17225)
  (issue, **open**, Mar 2026) — current-day request confirming kombu-only
  AMQP coverage ("Currently, support is only offered for kombu … Add
  support for aio-pika"; sibling closed request
  [#2048](https://github.com/DataDog/dd-trace-py/issues/2048)).

### `ot-baggage-*`: three tracers carry it everywhere, four never emit it

Measured: java/node/go ✅ on all 16 boundaries; python/dotnet/ruby/php/rust
❌ on all. The seeded `ot-baggage-*` header is ignored (not extracted, not
re-emitted) by the second group — the values still flow via W3C `baggage`
where that works.

- **dd-trace-py**: `ot-baggage-` inject *and* extract are gated on a
  non-default flag —
  [`propagation/http.py`](https://github.com/DataDog/dd-trace-py/blob/main/ddtrace/propagation/http.py)
  (source): `if config._propagation_http_baggage_enabled is True …`;
  [configuration docs](https://ddtrace.readthedocs.io/en/stable/configuration.html):
  "`DD_TRACE_PROPAGATION_HTTP_BAGGAGE_ENABLED` … default: False …
  version_added: v2.4.0" (introduced by
  [PR #6072](https://github.com/DataDog/dd-trace-py/pull/6072), explicitly
  "to have baggage item support … compatible with other language DataDog
  tracing libs e.g. java, nodejs"). The OpenTracing bridge — the other
  historical `ot-baggage` path — was removed entirely in
  [v3.0.0](https://github.com/DataDog/dd-trace-py/releases/tag/v3.0.0).
- **dd-trace-rb**: had it via OpenTracing; removed in 2.0 —
  [PR #3443](https://github.com/DataDog/dd-trace-rb/pull/3443) (merged Feb
  2024): "Remove opentracing support … OpenTracing is no longer supported
  from 2+." Maintainer confirmation in
  [#4055](https://github.com/DataDog/dd-trace-rb/issues/4055): "At the
  moment the Ruby tracer supports neither an OpenTracing or OpenTelemetry
  implementation of baggage … we previously removed the OpenTracing
  implementation since it's EOL." (W3C baggage landed later, 2.13.0, via
  [#4493](https://github.com/DataDog/dd-trace-rb/pull/4493)/[#4505](https://github.com/DataDog/dd-trace-rb/pull/4505).)
  Code search for `ot-baggage` in dd-trace-rb: 0 results.
- **dd-trace-dotnet**: never had it — code search for `ot-baggage` across
  the repo returns **zero** results; documented-by-absence.
- **The style itself is undocumented**: Datadog's
  [trace context propagation reference](https://docs.datadoghq.com/tracing/trace_collection/trace_context_propagation/)
  lists formats `datadog`, `tracecontext`, `b3single`, `b3multi`, `baggage`,
  `none` — `ot-baggage` appears nowhere. The headers dd-trace-java/js/go
  emit are legacy behavior of their `datadog` style, not a configurable,
  documented format.
- Nuance: [dd-trace-php#3614](https://github.com/DataDog/dd-trace-php/issues/3614)
  (issue, open) shows dd-trace-php *can* emit `ot-baggage-*` in some
  configurations (nginx rejecting `ot-baggage-true-client-ip`) — so "never
  emits" would overstate; we measured ❌ on all 16 boundaries at 1.25.0.

### Ruby: no bunny integration

- [Datadog Ruby compatibility docs](https://docs.datadoghq.com/tracing/trace_collection/compatibility/ruby/)
  (docs) — bunny has zero mentions; AMQP coverage exists only via the
  Sneakers/Kicks worker frameworks. Kafka rows are ruby-kafka, karafka,
  racecar (client coverage comes via karafka's rdkafka/waterdrop stack).
- [dd-trace-rb contrib directory](https://github.com/DataDog/dd-trace-rb/tree/master/lib/datadog/tracing/contrib)
  (source) — no `bunny` entry exists in code.

**Not found upstream**: no feature request for bunny exists at all — the
only AMQP-adjacent issue is the sneakers→kicks rename
([#4191](https://github.com/DataDog/dd-trace-rb/issues/4191)). Filing
candidate.

### PHP: no gRPC; rdkafka consume closes its span before user code runs

- [dd-trace-php#916](https://github.com/DataDog/dd-trace-php/issues/916)
  (issue, **open since 2020**) — the canonical gRPC request, deprioritized:
  > "We do not have enough bandwidth to work on this integration during this
  > quarter … I just added the label `up-for-grabs`." (labbati, Datadog)
  A user comment states the propagation consequence: "you will have no
  distributed tracing. For that we need to be able to exchange span and
  trace id via grpc metadata."
- [KafkaIntegration.php](https://github.com/DataDog/dd-trace-php/blob/master/src/DDTrace/Integrations/Kafka/KafkaIntegration.php)
  (source) — the consume-side root cause, visible in the hook: it opens a
  new trace root, attaches the extracted headers to *that span*, and calls
  `close_span()` **inside the `consume()` call itself** — before application
  code touches the message, so the relay's outbound calls run outside the
  extracted context. The flag is on by default
  ([configuration.h](https://github.com/DataDog/dd-trace-php/blob/master/tracer/configuration.h):
  `CONFIG(BOOL, DD_TRACE_KAFKA_DISTRIBUTED_TRACING, "true")`) — the gap is
  the scoping, not the flag.
- [Datadog PHP compatibility docs](https://docs.datadoghq.com/tracing/trace_collection/compatibility/php/)
  (docs) — gRPC absent from the library tables; RabbitMQ covered:
  "php-amqplib 2.x, 3.x".
- [dd-trace-php#639](https://github.com/DataDog/dd-trace-php/issues/639)
  (issue, open since 2019) — the historical rdkafka request; injection has
  since shipped but the issue was never closed.

**Not found upstream**: no issue reports that consumed Kafka context doesn't
reach post-consume user code in dd-trace-php — the source citation is the
only available evidence. Filing candidate.

### Go Orchestrion: every Kafka-touching leg starts a new trace; no amqp091 integration

Variant `datadog-go-orchestrion` (naive app, `orchestrion go build`, same
IBM/sarama v1.60.2 + dd-trace-go v2.10.1 as the hand-wired row):
http/grpc→http/grpc legs propagate everything (W3C baggage *and*
`ot-baggage-*`); every leg into or out of Kafka shows `x-datadog-*` +
`traceparent` present but carrying a fresh trace-id, baggage gone.

- [Orchestrion](https://github.com/DataDog/orchestrion) (repo, v1.13.0
  measured); [compile-time instrumentation docs](https://docs.datadoghq.com/tracing/trace_collection/automatic_instrumentation/dd_libraries/go/?tab=compiletimeinstrumentation).
- [orchestrion#871](https://github.com/DataDog/orchestrion/issues/871)
  (issue, open) — the context-discovery limitation underneath the produce
  gap: advice templates resolve a `context.Context` only from the innermost
  function's parameter list. sarama's `SyncProducer.SendMessage(msg)` has no
  context parameter at all, so the woven produce span cannot attach to the
  inbound request context and starts a new root — exactly what the matrix
  records.
- [dd-trace-go contrib IBM/sarama](https://pkg.go.dev/github.com/DataDog/dd-trace-go/contrib/IBM/sarama/v2)
  (docs) — the manual pattern the hand-wired relay uses closes both gaps:
  inject from the request context into the message before `SendMessage`,
  extract-and-activate in the consumer handler. Same library versions, all
  16 legs green — the delta is purely the two steps the weaver cannot see.
- dd-trace-go has no amqp091/RabbitMQ contrib (see §Datadog/Ruby economics
  in FINDINGS), so Orchestrion has nothing to weave on rabbitmq legs.

**Not found upstream**: (a) no issue reports that Orchestrion's sarama
consume path fails to continue the trace seeded in record headers — filing
candidate; (b) the published `dd-trace-go/orchestrion/all/v2` module (at
v2.10.1) carries dangling local `replace` directives that make
`orchestrion pin` print a spurious "removing unnecessary import" warning
(build unaffected; verified by symbol inspection) — filing candidate.

---

## Elastic APM

### Python: `tracestate: tracestate` corruption after gRPC-inbound — one-line source bug, unreported

- [server_interceptor.py @ v6.26.2, lines 66–73](https://github.com/elastic/apm-agent-python/blob/v6.26.2/elasticapm/contrib/grpc/server_interceptor.py#L66-L73)
  (source, at the tested release) — the bug is a one-liner in
  `get_trace_parent()`: the header **name** is assigned instead of its
  value:

```70:71:elasticapm/contrib/grpc/server_interceptor.py
            elif metadata.key == "tracestate":
                tracestate = metadata.key
```

  Still [unfixed on main](https://github.com/elastic/apm-agent-python/blob/main/elasticapm/contrib/grpc/server_interceptor.py#L70-L71)
  (file last touched Oct 2023). Bonus finding for the same report: the async
  variant (`async_server_interceptor.py`) never reads `invocation_metadata`
  at all — the async gRPC server path doesn't extract trace context, period.

**Not found upstream**: no issue mentions gRPC tracestate handling at all —
filing candidate with the one-line fix (`metadata.key` → `metadata.value`).

### Python: no confluent-kafka / pika instrumentation

- [Python supported technologies](https://www.elastic.co/docs/reference/apm/agents/python/supported-technologies)
  (docs) — the only messaging client on the page is `kafka-python`
  (`>=2.0,<3`); confluent-kafka and pika appear nowhere.
- [apm-agent-python#678](https://github.com/elastic/apm-agent-python/issues/678)
  (issue, **open since Dec 2019**) — "It would be of high value to add
  instrumentation and distributed tracing for message brokers and, in
  particular, RabbitMQ."

### Node: kafkajs yes, amqplib no

- [Node supported technologies](https://www.elastic.co/docs/reference/apm/agents/nodejs/supported-technologies)
  (docs) — kafkajs `>=2.0.0 <3.0.0` in the table; amqplib absent from the
  page. The [distributed-tracing docs](https://www.elastic.co/docs/reference/apm/agents/nodejs/distributed-tracing)
  direct queue users to manual `traceparent` passing (the recipe carries
  `traceparent` only — no tracestate or baggage).
- [apm-agent-nodejs#1188](https://github.com/elastic/apm-agent-nodejs/issues/1188)
  (issue, **open since Jul 2019**) and
  [#1791](https://github.com/elastic/apm-agent-nodejs/issues/1791)
  (issue, **open since Jul 2020**) — long-standing amqplib requests.

### Ruby: no zero-code gRPC; Kafka only via Racecar (which can't continue a trace)

- [Ruby supported technologies](https://www.elastic.co/docs/reference/apm/agents/ruby/supported-technologies)
  (docs) — gRPC support exists but is *experimental and manual*: "To
  instrument a client, add the `ElasticAPM::GRPC::ClientInterceptor` as an
  `interceptor` at Stub creation" — so the accurate claim is "no zero-code
  gRPC", not "no gRPC at all". RabbitMQ appears only as "Sneakers
  (Experimental)"; no bunny, no Kafka client library.
- [racecar.rb spy source](https://github.com/elastic/apm-agent-ruby/blob/main/lib/elastic_apm/spies/racecar.rb)
  (source; added by [PR #1284](https://github.com/elastic/apm-agent-ruby/pull/1284))
  — even the one supported Kafka framework never reads message headers: it
  calls `ElasticAPM.start_transaction(kind, TYPE)` with no `trace_parent`,
  so Kafka consume cannot continue a trace even under Racecar.

**Not found upstream**: no issues requesting bunny or client-level Kafka
(rdkafka/ruby-kafka) instrumentation.

### .NET: messaging is profiler-deployment-only, x64-only

- [.NET profiler auto instrumentation docs](https://www.elastic.co/docs/reference/apm/agents/dotnet/setup-auto-instrumentation)
  (docs) — in the technology table, **Kafka and RabbitMQ are the only rows
  without the startup-hook marker** (profiler IL-rewriting only), with caps
  "Confluent.Kafka ≥1.4.0 <3.0.0", "RabbitMQ.Client ≥3.6.9 <7.0.0", and
  "ARM and 32-bit processes are not supported." (Correction to earlier
  phrasing: the profiler runs on Windows *and* Linux — x64-only, not
  Linux-only.)

### Go: no baggage, by explicit decision

- [apmot/doc.go](https://github.com/elastic/apm-agent-go/blob/main/module/apmot/doc.go)
  (source) — "Things not implemented by this tracer: binary propagation
  format, **baggage**, logging";
  [apmot/span.go](https://github.com/elastic/apm-agent-go/blob/main/module/apmot/span.go#L127-L136):
  "SetBaggageItem is a no-op; we do not support baggage."
- [apm-agent-go#537](https://github.com/elastic/apm-agent-go/issues/537)
  (issue, closed 2019 not-planned) — maintainer: "no plans for baggage at
  the moment." A repo-wide search hits baggage only in the OpenTracing
  bridge — there is no W3C baggage code anywhere, putting the Go agent out
  of compliance with Elastic's own agent spec ("Agents MUST parse, validate,
  and attach the `baggage` header according to the W3C specification").

### The `elasticapmtraceparent` binary header

- [elastic/apm agent spec — distributed tracing](https://github.com/elastic/apm/blob/main/specs/agents/tracing-distributed-tracing.md)
  (spec) — dual-emit on messaging is mandated for backward compatibility:
  > "Add a `elasticapmtraceparent` header with the binary specification
  > above for backwards compatibility; Add the textual `traceparent` and
  > `tracestate` headers… We will eventually remove the
  > `elasticapmtraceparent` entirely."

---

## New Relic

### No baggage, anywhere — confirmed by omission

- [How New Relic distributed tracing works](https://docs.newrelic.com/docs/distributed-tracing/concepts/how-new-relic-distributed-tracing-works/)
  (docs) — defines the complete header model as exactly three headers
  (`traceparent`, `tracestate`, `newrelic`); baggage does not appear.
- Org-wide GitHub search for "baggage" across all New Relic agent repos:
  three hits, all unrelated. **No feature request for W3C baggage exists**
  in any of the seven agent repos.

### Java: Kafka DT opt-in; no spring-rabbit module at all

- [Java agent Kafka docs](https://docs.newrelic.com/docs/apm/agents/java-agent/instrumentation/java-agent-instrument-kafka-message-queues/)
  (docs) — Kafka DT header injection requires
  `class_transformer: com.newrelic.instrumentation.kafka-clients-spans-0.11.0.0: enabled: true`
  ("The instrumentation adds a 150 to 200 byte payload to message headers…
  we recommend testing… before enabling them in production"); the matching
  [instrumentation module](https://github.com/newrelic/newrelic-java-agent/tree/main/instrumentation/kafka-clients-spans-0.11.0.0)
  exists in the repo.
- [Consumer_Instrumentation.java (rabbit-amqp-5.0.0)](https://github.com/newrelic/newrelic-java-agent/blob/main/instrumentation/rabbit-amqp-5.0.0/src/main/java/com/rabbitmq/client/Consumer_Instrumentation.java)
  (source) — why RabbitMQ-inbound dies under Spring: the agent ships **no
  spring-rabbit module** (only `rabbit-amqp-2.7.0/5.0.0` and
  `spring-kafka-2.2.0`); DT accept happens inside a transaction woven around
  `Consumer.handleDelivery` on the AMQP connection thread, while
  spring-rabbit's listener container processes the message on a *different*
  thread, outside that transaction.

**Not found upstream**: no issue about the Java agent failing to accept DT
headers in spring-rabbit listeners or Kafka consumers.

### Node: kafkajs instrumentation is behind a default-off feature flag

- [feature_flags.js @ v14.3.10](https://github.com/newrelic/node-newrelic/blob/v14.3.10/lib/feature_flags.js)
  (source, tested release) — `kafkajs_instrumentation: false` (still false
  on main); with the flag off the agent logs
  ["skipping instrumentation of kafkajs"](https://github.com/newrelic/node-newrelic/blob/v14.3.10/lib/subscribers/kafkajs/client-constructor.js#L56-L64)
  and does nothing at all.
- [feature-flags.md @ v14.3.10](https://github.com/newrelic/node-newrelic/blob/v14.3.10/documentation/feature-flags.md)
  (docs) — enable via `NEW_RELIC_FEATURE_FLAG_KAFKAJS_INSTRUMENTATION`;
  shipped behind the flag since
  [v11.19.0](https://github.com/newrelic/node-newrelic/releases/tag/v11.19.0)
  (Jun 2024) and never promoted to default. The
  [compatibility list](https://github.com/newrelic/node-newrelic/blob/main/compatibility.md)
  also caps kafkajs at 2.2.4.
- **Empirical confirmation** (2026-09-06): rerunning `newrelic-node` with
  `NEW_RELIC_FEATURE_FLAG_KAFKAJS_INSTRUMENTATION=true` flips all seven
  Kafka boundaries to ✅ (W3C headers, both directions) — 9/16 becomes
  16/16. Run archived at
  `results/experiments/newrelic-node-kafkajs-flag.json`.

### Go: nrsarama's producer injects into a throwaway map; its consumer never accepts

- [nrsarama/producer.go](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/producer.go)
  (source) — the injection bug: the carrier's `Set` (which would copy
  headers into `msg.Headers`) is bypassed because
  `pw.txn.InsertDistributedTraceHeaders(carrier.Header)` is called on the
  plain embedded `http.Header` — DT headers land in a discarded map and are
  never attached to the Kafka message. Explains all-Kafka-outbound-dead.
- [nrsarama/consumer.go](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/consumer.go)
  (source) — `ClaimIngestion` starts a fresh `kafkaconsumer` transaction and
  never calls `AcceptDistributedTraceHeaders`; `processMessage` even calls
  `InsertDistributedTraceHeaders` (outbound semantics) on the *consumed*
  headers. The
  [official example](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/example/consumer/consumerexample.go)
  contains no accept call either — our harness's manual
  `AcceptDistributedTraceHeaders` went beyond the documented flow.

**Not found upstream**: zero issues mention nrsarama propagation (only the
original 2019/2020 feature requests
[#98](https://github.com/newrelic/go-agent/issues/98)/[#160](https://github.com/newrelic/go-agent/issues/160)).
Both bugs are strong filing candidates.

### .NET: RabbitMQ.Client 7 is hybrid-agent-only

- [newrelic-dotnet-agent PR #2890](https://github.com/newrelic/newrelic-dotnet-agent/pull/2890)
  (PR, closed **unmerged** Nov 2024) — the abandoned async-API
  instrumentation: "Closing because of `ValueTask<T>` blocker".
- [newrelic-dotnet-agent#2885](https://github.com/newrelic/newrelic-dotnet-agent/issues/2885)
  (issue, closed Jun 2026 as resolved) — the reconciliation with the compat
  docs' "7.1.2 verified" claim: "RabbitMQ 7+ versions are supported with the
  new **hybrid agent** feature in agent versions 10.49.0+".
- [OpenTelemetry API support docs](https://docs.newrelic.com/docs/apm/agents/manage-apm-agents/opentelemetry-api-support/)
  (docs) — the hybrid agent (which picks up RabbitMQ.Client 7's native
  ActivitySource) is **disabled by default**, and even enabled, "RabbitMQ:
  Distributed tracing works differently (**span links vs. parent/child
  relationships**)."
- [.NET compatibility docs](https://docs.newrelic.com/docs/apm/agents/net-agent/getting-started/net-agent-compatibility-requirements/)
  (docs) — the instrumented-methods list is exclusively the classic
  `IModel.*` surface, which RabbitMQ.Client 7 removed entirely — so default
  config on 10.54.0 with `IChannel.BasicPublishAsync` is dead, exactly as
  measured.

### Node + Python emit W3C-only: `exclude_newrelic_header` flipped in 2026 majors

- [node-newrelic NEWS.md, v14.0.0](https://github.com/newrelic/node-newrelic/blob/main/NEWS.md)
  (changelog, May 2026, breaking) — "Updated
  `config.distributed_tracing.exclude_newrelic_header` to be set to `true`
  by default" ([PR #3980](https://github.com/newrelic/node-newrelic/pull/3980));
  confirmed in [default.js @ v14.3.10](https://github.com/newrelic/node-newrelic/blob/v14.3.10/lib/config/default.js#L1157-L1160).
- [newrelic-python-agent PR #1721](https://github.com/newrelic/newrelic-python-agent/pull/1721)
  (PR, merged May 2026, shipped v13.0.0) — "Exclude `newrelic` DT header by
  default"; confirmed in
  [config.py @ v13.5.0](https://github.com/newrelic/newrelic-python-agent/blob/v13.5.0/newrelic/core/config.py#L1182).
- Docs lag the code twice over: the
  [Node config docs](https://docs.newrelic.com/docs/apm/agents/nodejs-agent/installation-configuration/nodejs-agent-configuration/)
  still list the default as `false`, and the DT concepts page still says all
  agents send the `newrelic` header "by default". Java/Go/.NET/PHP genuinely
  still dual-emit, matching our matrix.

---

## Filing shortlist

Findings with **no upstream source** — each verified absent by search on
2026-09-06, each a candidate for us to file (with the evidence above):

1. **elastic/apm-agent-python** — gRPC server interceptor emits
   `tracestate: tracestate` (one-line fix: `metadata.key` →
   `metadata.value`); the async interceptor never extracts context at all.
2. **newrelic/go-agent** — nrsarama producer injects DT headers into a
   discarded map (carrier `Set` bypassed); nrsarama consumer (and official
   example) never calls `AcceptDistributedTraceHeaders`.
3. **open-telemetry/opentelemetry-php** — ExtRdKafka `producev` injection
   silently no-ops without an explicit `$headers` argument unless
   `opentelemetry.allow_stack_extension=On`; neither is in the
   instrumentation README.
4. **DataDog/dd-trace-php** — rdkafka consume closes its span inside
   `consume()`, so extracted context never reaches user code.
5. **DataDog/dd-trace-java** — baggage loss on gRPC/RabbitMQ/JMS boundaries
   (only Kafka is reported, in #11286).
6. **open-telemetry/opentelemetry-python-contrib** — baggage dimension of
   the confluent-kafka consume gap (existing issues cover trace identity
   only). Same for **opentelemetry-dotnet-instrumentation** (Kafka baggage).
7. **Feature requests that don't exist**: bunny integration for
   DataDog/dd-trace-rb; php-amqplib instrumentation for OTel PHP; server-side
   `grpc` gem instrumentation for OTel Ruby; `ext-grpc` client
   instrumentation for OTel PHP; W3C baggage for any New Relic agent.
