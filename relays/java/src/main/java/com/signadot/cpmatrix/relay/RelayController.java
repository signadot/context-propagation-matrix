package com.signadot.cpmatrix.relay;

import org.apache.kafka.common.utils.AppInfoParser;
import org.springframework.boot.SpringBootVersion;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.Map;

@RestController
public class RelayController {

    private final Forwarder forwarder;

    public RelayController(Forwarder forwarder) {
        this.forwarder = forwarder;
    }

    public record RelayReq(String test_id, String forward_via) {}

    @PostMapping("/relay")
    public Map<String, String> relay(@RequestBody RelayReq req) {
        forwarder.forward(req.test_id(), req.forward_via());
        return Map.of("status", "forwarded");
    }

    @GetMapping("/info")
    public Map<String, Object> info() {
        Map<String, Object> out = new LinkedHashMap<>();
        out.put("variant", Forwarder.env("VARIANT", "unknown"));
        out.put("tool", Forwarder.env("TOOL", "unknown"));
        out.put("language", "java");
        out.put("tracer_version", tracerVersion());
        out.put("runtime_version", "java " + System.getProperty("java.version"));

        Map<String, String> config = new LinkedHashMap<>();
        for (String key : new String[]{
                "OTEL_PROPAGATORS", "OTEL_TRACES_EXPORTER",
                "DD_TRACE_PROPAGATION_STYLE",
                "NEW_RELIC_DISTRIBUTED_TRACING_ENABLED",
                "ELASTIC_APM_SERVER_URL"}) {
            String v = System.getenv(key);
            if (v != null && !v.isEmpty()) {
                config.put(key, v);
            }
        }
        out.put("config", config);

        Map<String, String> libs = new LinkedHashMap<>();
        libs.put("spring-boot", SpringBootVersion.getVersion());
        libs.put("kafka-clients", AppInfoParser.getVersion());
        libs.put("http-client", "RestTemplate (HttpURLConnection)");
        libs.put("grpc", "grpc-java");
        libs.put("kafka", "spring-kafka");
        libs.put("rabbitmq", "spring-rabbit (amqp-client "
                + com.rabbitmq.client.impl.ClientVersion.VERSION + ")");
        out.put("libs", libs);
        return out;
    }

    private String tracerVersion() {
        String file = Forwarder.env("TRACER_VERSION_FILE", "/agent/version.txt");
        try {
            return Files.readString(Path.of(file)).trim();
        } catch (Exception e) {
            return "unknown";
        }
    }
}
