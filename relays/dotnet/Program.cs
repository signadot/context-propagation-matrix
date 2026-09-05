// Relay app for the context-propagation matrix (see DESIGN.md).
//
// Deliberately naive application code: it never touches HTTP headers, gRPC
// metadata, Kafka record headers, AMQP header tables, Activity, or any tracing
// API. Whatever context propagation happens is exclusively the job of the
// attached auto-instrumentation (OpenTelemetry / Datadog / New Relic / Elastic
// APM zero-code agents).

using System.Net.Http.Json;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Text.Json;
using System.Text.Json.Serialization;
using Confluent.Kafka;
using CpMatrix;
using Grpc.Core;
using Grpc.Net.Client;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using RabbitMQ.Client;
using RabbitMQ.Client.Events;

var builder = WebApplication.CreateBuilder(args);

// :8080 REST (HTTP/1.1), :8081 gRPC (HTTP/2 plaintext / h2c).
builder.WebHost.ConfigureKestrel(kestrel =>
{
    kestrel.ListenAnyIP(8080, o => o.Protocols = HttpProtocols.Http1);
    kestrel.ListenAnyIP(8081, o => o.Protocols = HttpProtocols.Http2);
});

builder.Services.AddGrpc();
builder.Services.AddHttpClient();
builder.Services.AddSingleton<Forwarder>();
builder.Services.AddHostedService<KafkaRelayConsumer>();
builder.Services.AddHostedService<RabbitRelayConsumer>();

var app = builder.Build();

app.MapPost("/relay", async (RelayReq req, Forwarder forwarder) =>
{
    await forwarder.ForwardAsync(req.TestId, req.ForwardVia);
    return Results.Json(new Dictionary<string, string> { ["status"] = "forwarded" });
});

app.MapGet("/info", () =>
{
    string tracerVersion;
    try
    {
        tracerVersion = File.ReadAllText(
            EnvUtil.Get("TRACER_VERSION_FILE", "/agent/version.txt")).Trim();
    }
    catch
    {
        tracerVersion = "";
    }
    if (tracerVersion.Length == 0)
    {
        tracerVersion = "unknown";
    }

    var config = new Dictionary<string, string>();
    foreach (var key in new[]
             {
                 "OTEL_PROPAGATORS", "OTEL_TRACES_EXPORTER",
                 "DD_TRACE_PROPAGATION_STYLE", "NEW_RELIC_DISTRIBUTED_TRACING_ENABLED",
             })
    {
        if (Environment.GetEnvironmentVariable(key) is { Length: > 0 } value)
        {
            config[key] = value;
        }
    }

    static string Ver(Type t) =>
        t.Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()
            ?.InformationalVersion.Split('+')[0]
        ?? t.Assembly.GetName().Version?.ToString() ?? "unknown";

    return Results.Json(new Dictionary<string, object>
    {
        ["variant"] = EnvUtil.Get("VARIANT", "unknown"),
        ["tool"] = EnvUtil.Get("TOOL", "unknown"),
        ["language"] = "dotnet",
        ["tracer_version"] = tracerVersion,
        ["runtime_version"] = $"{RuntimeInformation.FrameworkDescription} (CLR {Environment.Version})",
        ["config"] = config,
        ["libs"] = new Dictionary<string, string>
        {
            ["http"] = "HttpClient",
            ["grpc"] = $"Grpc.AspNetCore + Grpc.Net.Client {Ver(typeof(GrpcChannel))}",
            ["kafka"] = $"Confluent.Kafka {Ver(typeof(ConsumerConfig))}",
            ["rabbitmq"] = $"RabbitMQ.Client {Ver(typeof(ConnectionFactory))}",
        },
    });
});

app.MapGrpcService<GrpcRelayService>();

app.Run();

// ---------------------------------------------------------------------------

record RelayReq(
    [property: JsonPropertyName("test_id")] string TestId,
    [property: JsonPropertyName("forward_via")] string ForwardVia);

static class EnvUtil
{
    public static string Get(string key, string dflt) =>
        Environment.GetEnvironmentVariable(key) is { Length: > 0 } v ? v : dflt;
}

// Forwards a message to the sink over the requested protocol. Never touches
// headers, metadata, record headers or tracing APIs.
sealed class Forwarder(IHttpClientFactory httpFactory)
{
    private readonly Lazy<IProducer<Null, string>> _producer = new(() =>
        new ProducerBuilder<Null, string>(new ProducerConfig
        {
            BootstrapServers = EnvUtil.Get("KAFKA_BROKERS", "kafka:9092"),
        }).Build());

    private readonly Lazy<Sink.SinkClient> _sink = new(() =>
    {
        var target = EnvUtil.Get("SINK_GRPC", "sink:9081");
        var address = target.Contains("://") ? target : "http://" + target;
        // http:// address => plaintext HTTP/2 (h2c), no extra config on .NET 8.
        return new Sink.SinkClient(GrpcChannel.ForAddress(address));
    });

    private static string SinkRabbitQueue => EnvUtil.Get("SINK_RABBIT_QUEUE", "sink-rmq");

    private readonly Lazy<Task<IChannel>> _rabbit = new(async () =>
    {
        var factory = new ConnectionFactory
        {
            Uri = new Uri(EnvUtil.Get("RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/")),
        };
        var connection = await factory.CreateConnectionAsync();
        var channel = await connection.CreateChannelAsync();
        // Defensive declare with the harness-wide queue properties (idempotent).
        await channel.QueueDeclareAsync(SinkRabbitQueue,
            durable: false, exclusive: false, autoDelete: false);
        return channel;
    });

    public async Task ForwardAsync(string testId, string via)
    {
        switch (via)
        {
            case "http":
                var response = await httpFactory.CreateClient().PostAsJsonAsync(
                    EnvUtil.Get("SINK_HTTP", "http://sink:9080") + "/deliver",
                    new Dictionary<string, string> { ["test_id"] = testId });
                response.EnsureSuccessStatusCode();
                break;
            case "grpc":
                await _sink.Value.DeliverAsync(new SinkRequest { TestId = testId });
                break;
            case "kafka":
                await _producer.Value.ProduceAsync("sink-in", new Message<Null, string>
                {
                    Value = JsonSerializer.Serialize(
                        new Dictionary<string, string> { ["test_id"] = testId }),
                });
                break;
            case "rabbitmq":
                var channel = await _rabbit.Value;
                // Content type only; whether tracing entries appear in the
                // AMQP header table is up to the instrumentation.
                await channel.BasicPublishAsync(
                    exchange: "",
                    routingKey: SinkRabbitQueue,
                    mandatory: false,
                    basicProperties: new BasicProperties { ContentType = "application/json" },
                    body: JsonSerializer.SerializeToUtf8Bytes(
                        new Dictionary<string, string> { ["test_id"] = testId }));
                break;
            default:
                throw new ArgumentException($"unknown forward_via: {via}");
        }
    }
}

sealed class GrpcRelayService(Forwarder forwarder) : Relay.RelayBase
{
    public override async Task<RelayResponse> Handle(
        RelayRequest request, ServerCallContext context)
    {
        await forwarder.ForwardAsync(request.TestId, request.ForwardVia);
        return new RelayResponse { Status = "forwarded" };
    }
}

// Consumes {"test_id","forward_via"} JSON from RELAY_TOPIC and forwards.
// Messages arrive carrying tracing record headers; they are never read here —
// the instrumentation is responsible for extracting them and re-injecting
// context on the outbound call.
sealed class KafkaRelayConsumer(Forwarder forwarder, ILogger<KafkaRelayConsumer> logger)
    : BackgroundService
{
    protected override Task ExecuteAsync(CancellationToken stoppingToken) =>
        // Consume() blocks, so run the loop on its own thread and let host
        // startup (Kestrel) proceed.
        Task.Run(() => ConsumeLoop(stoppingToken), CancellationToken.None);

    private void ConsumeLoop(CancellationToken ct)
    {
        var config = new ConsumerConfig
        {
            BootstrapServers = EnvUtil.Get("KAFKA_BROKERS", "kafka:9092"),
            GroupId = "relay-" + EnvUtil.Get("VARIANT", "unknown"),
            AutoOffsetReset = AutoOffsetReset.Earliest,
        };
        using var consumer = new ConsumerBuilder<Ignore, string>(config).Build();
        consumer.Subscribe(EnvUtil.Get("RELAY_TOPIC", "relay-in"));
        logger.LogInformation("kafka consumer started");
        while (!ct.IsCancellationRequested)
        {
            try
            {
                var result = consumer.Consume(ct);
                if (result?.Message?.Value is not { } value)
                {
                    continue;
                }
                var req = JsonSerializer.Deserialize<RelayReq>(value);
                if (req is not null)
                {
                    // Forward inside the consume loop so the instrumentation's
                    // notion of "current" consumer context is active.
                    forwarder.ForwardAsync(req.TestId, req.ForwardVia)
                        .GetAwaiter().GetResult();
                }
            }
            catch (OperationCanceledException)
            {
                break;
            }
            catch (Exception ex)
            {
                logger.LogError(ex, "kafka message handling failed");
            }
        }
        consumer.Close();
    }
}

// Consumes {"test_id","forward_via"} JSON from RELAY_RABBIT_QUEUE and forwards.
// Deliveries arrive carrying tracing entries in the AMQP header table; they
// are never read here — the instrumentation is responsible for extracting
// them and re-injecting context on the outbound call.
sealed class RabbitRelayConsumer(Forwarder forwarder, ILogger<RabbitRelayConsumer> logger)
    : BackgroundService
{
    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        var queue = EnvUtil.Get(
            "RELAY_RABBIT_QUEUE", "relay-rmq-" + EnvUtil.Get("VARIANT", "unknown"));
        var factory = new ConnectionFactory
        {
            Uri = new Uri(EnvUtil.Get("RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/")),
        };

        try
        {
            // The broker may not be up yet; retry until it is (compose gates
            // on the rabbitmq healthcheck, so this is just defensive).
            IConnection connection;
            while (true)
            {
                try
                {
                    connection = await factory.CreateConnectionAsync(stoppingToken);
                    break;
                }
                catch (Exception ex) when (ex is not OperationCanceledException)
                {
                    logger.LogWarning("rabbitmq not reachable ({Message}), retrying", ex.Message);
                    await Task.Delay(TimeSpan.FromSeconds(2), stoppingToken);
                }
            }

            var channel = await connection.CreateChannelAsync(cancellationToken: stoppingToken);
            await channel.QueueDeclareAsync(queue,
                durable: false, exclusive: false, autoDelete: false,
                cancellationToken: stoppingToken);

            var consumer = new AsyncEventingBasicConsumer(channel);
            consumer.ReceivedAsync += async (_, delivery) =>
            {
                try
                {
                    var req = JsonSerializer.Deserialize<RelayReq>(delivery.Body.Span);
                    if (req is not null)
                    {
                        // Forward inside the handler so the instrumentation's
                        // notion of "current" consumer context is active.
                        await forwarder.ForwardAsync(req.TestId, req.ForwardVia);
                    }
                }
                catch (Exception ex)
                {
                    logger.LogError(ex, "rabbitmq message handling failed");
                }
            };
            await channel.BasicConsumeAsync(queue, autoAck: true, consumer, stoppingToken);
            logger.LogInformation("rabbitmq consumer started");

            // Keep the connection and channel rooted until shutdown.
            await Task.Delay(Timeout.InfiniteTimeSpan, stoppingToken);
        }
        catch (OperationCanceledException)
        {
            // Host is shutting down; sockets are closed with the process.
        }
    }
}
