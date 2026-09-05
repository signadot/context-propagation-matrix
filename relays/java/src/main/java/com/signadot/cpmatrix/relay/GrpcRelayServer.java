package com.signadot.cpmatrix.relay;

import com.signadot.cpmatrix.RelayGrpc;
import com.signadot.cpmatrix.RelayRequest;
import com.signadot.cpmatrix.RelayResponse;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.stub.StreamObserver;
import org.springframework.boot.ApplicationArguments;
import org.springframework.boot.ApplicationRunner;
import org.springframework.stereotype.Component;

@Component
public class GrpcRelayServer implements ApplicationRunner {

    private final Forwarder forwarder;
    private Server server;

    public GrpcRelayServer(Forwarder forwarder) {
        this.forwarder = forwarder;
    }

    @Override
    public void run(ApplicationArguments args) throws Exception {
        server = ServerBuilder.forPort(8081)
                .addService(new RelayGrpc.RelayImplBase() {
                    @Override
                    public void handle(RelayRequest req, StreamObserver<RelayResponse> resp) {
                        forwarder.forward(req.getTestId(), req.getForwardVia());
                        resp.onNext(RelayResponse.newBuilder().setStatus("forwarded").build());
                        resp.onCompleted();
                    }
                })
                .build()
                .start();
    }
}
