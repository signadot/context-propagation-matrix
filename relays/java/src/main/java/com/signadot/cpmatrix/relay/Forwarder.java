package com.signadot.cpmatrix.relay;

import tools.jackson.databind.ObjectMapper;
import tools.jackson.databind.json.JsonMapper;
import com.signadot.cpmatrix.SinkGrpc;
import com.signadot.cpmatrix.SinkRequest;
import io.grpc.Grpc;
import io.grpc.InsecureChannelCredentials;
import io.grpc.ManagedChannel;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.kafka.core.KafkaTemplate;
import org.springframework.stereotype.Service;
import org.springframework.web.client.RestTemplate;

import java.util.Map;

/**
 * Forwards a message to the sink over the requested protocol.
 *
 * Deliberately naive: never touches headers, metadata or tracing APIs.
 * Whatever context propagation happens is the instrumentation agent's doing.
 */
@Service
public class Forwarder {

    private final RestTemplate http = new RestTemplate();
    private final KafkaTemplate<String, String> kafka;
    private final RabbitTemplate rabbit;
    private final ObjectMapper om = JsonMapper.builder().build();

    private final String sinkHttp = env("SINK_HTTP", "http://sink:9080");
    private final String sinkGrpc = env("SINK_GRPC", "sink:9081");
    private final String sinkRabbitQueue;

    private volatile SinkGrpc.SinkBlockingStub sinkStub;

    public Forwarder(KafkaTemplate<String, String> kafka, RabbitTemplate rabbit,
                     @Value("${cpmatrix.sink-rabbit-queue}") String sinkRabbitQueue) {
        this.kafka = kafka;
        this.rabbit = rabbit;
        this.sinkRabbitQueue = sinkRabbitQueue;
    }

    public void forward(String testId, String via) {
        try {
            switch (via) {
                case "http" -> http.postForEntity(sinkHttp + "/deliver",
                        Map.of("test_id", testId), String.class);
                case "grpc" -> stub().deliver(
                        SinkRequest.newBuilder().setTestId(testId).build());
                case "kafka" -> kafka.send("sink-in",
                        om.writeValueAsString(Map.of("test_id", testId)));
                case "rabbitmq" -> rabbit.convertAndSend(sinkRabbitQueue,
                        om.writeValueAsString(Map.of("test_id", testId)));
                default -> throw new IllegalArgumentException("unknown forward_via: " + via);
            }
        } catch (RuntimeException e) {
            throw e;
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    private SinkGrpc.SinkBlockingStub stub() {
        if (sinkStub == null) {
            synchronized (this) {
                if (sinkStub == null) {
                    ManagedChannel ch = Grpc.newChannelBuilder(
                            sinkGrpc, InsecureChannelCredentials.create()).build();
                    sinkStub = SinkGrpc.newBlockingStub(ch);
                }
            }
        }
        return sinkStub;
    }

    static String env(String key, String dflt) {
        String v = System.getenv(key);
        return v == null || v.isEmpty() ? dflt : v;
    }
}
