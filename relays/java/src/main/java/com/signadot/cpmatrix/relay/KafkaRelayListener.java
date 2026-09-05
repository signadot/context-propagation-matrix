package com.signadot.cpmatrix.relay;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.springframework.kafka.annotation.KafkaListener;
import org.springframework.stereotype.Component;

@Component
public class KafkaRelayListener {

    private final Forwarder forwarder;
    private final ObjectMapper om = new ObjectMapper();

    public KafkaRelayListener(Forwarder forwarder) {
        this.forwarder = forwarder;
    }

    @KafkaListener(topics = "${cpmatrix.relay-topic}")
    public void onMessage(String value) throws Exception {
        RelayController.RelayReq req = om.readValue(value, RelayController.RelayReq.class);
        forwarder.forward(req.test_id(), req.forward_via());
    }
}
