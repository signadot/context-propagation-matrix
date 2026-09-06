# Upstream references — evidence for every non-success case

Companion to [`FINDINGS.md`](FINDINGS.md): each failing (or surprising) cell
family in [`results/MATRIX.md`](results/MATRIX.md) mapped to upstream
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

---

## Datadog

*(research in progress)*

---

## Elastic APM

*(research in progress)*

---

## New Relic

*(research in progress)*
