package com.signadot.cpmatrix.relay;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.springframework.amqp.core.Queue;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.stereotype.Component;

/**
 * RabbitMQ inbound leg. Queue properties per the harness contract:
 * durable=false, auto-delete=false, exclusive=false (must match the driver's
 * declare). Spring's RabbitAdmin declares the beans below at startup.
 */
@Component
public class RabbitRelayListener {

    private final Forwarder forwarder;
    private final ObjectMapper om = new ObjectMapper();

    public RabbitRelayListener(Forwarder forwarder) {
        this.forwarder = forwarder;
    }

    @RabbitListener(queues = "${cpmatrix.relay-rabbit-queue}")
    public void onMessage(String body) throws Exception {
        RelayController.RelayReq req = om.readValue(body, RelayController.RelayReq.class);
        forwarder.forward(req.test_id(), req.forward_via());
    }

    @Configuration
    static class Queues {
        @Bean
        Queue relayRabbitQueue(@Value("${cpmatrix.relay-rabbit-queue}") String name) {
            return new Queue(name, false);
        }

        @Bean
        Queue sinkRabbitQueue(@Value("${cpmatrix.sink-rabbit-queue}") String name) {
            return new Queue(name, false);
        }
    }
}
