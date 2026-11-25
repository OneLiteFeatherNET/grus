package net.onelitefeather.grus.sdk;

import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.util.Optional;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;

class GrusClientNetworkTest {

    @Test
    void networkReadsNetworkStatusFromGetV1Network() throws IOException {
        try (StubServer server = new StubServer()) {
            AtomicReference<String> method = new AtomicReference<>();
            server.route("/v1/network", exchange -> {
                method.set(exchange.getRequestMethod());
                StubServer.respond(exchange, 200, "{\"players\":42,\"maxPlayers\":1000,\"proxies\":3}");
            });
            GrusClient client = new GrusClient(server.config());

            Optional<NetworkStatus> status = client.network();

            assertEquals("GET", method.get(), "the network count is read with GET");
            assertEquals(Optional.of(new NetworkStatus(42, 1000, 3)), status, "the body must be parsed");
        }
    }

    @Test
    void networkIsEmptyWhenControllerPredatesTheEndpoint() throws IOException {
        try (StubServer server = new StubServer()) {
            server.route("/v1/network", exchange -> StubServer.respond(exchange, 404, "{\"error\":\"not found\"}"));
            GrusClient client = new GrusClient(server.config());

            Optional<NetworkStatus> status = client.network();

            assertFalse(status.isPresent(), "a 404 from an older controller must mean no data, not a failure");
        }
    }

    @Test
    void networkPropagatesServerErrors() throws IOException {
        try (StubServer server = new StubServer()) {
            server.route("/v1/network", exchange -> StubServer.respond(exchange, 500, "boom"));
            GrusClient client = new GrusClient(server.config());

            GrusException error = assertThrows(GrusException.class, client::network,
                    "only a 404 is treated as missing; other failures must reach the caller");

            assertEquals(500, error.status(), "the HTTP status must be kept");
        }
    }
}
