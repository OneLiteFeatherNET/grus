package net.onelitefeather.grus.sdk;

import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

class GrusClientReadyRetryTest {

    private static final String READY_JSON = """
            {"id":"lobby-1","pool":"lobby","role":"game","state":"Starting","address":"10.0.0.1:25565",
             "players":0,"maxPlayers":20,"labels":{}}""";

    @Test
    void readyWithRetryReturnsInstanceAfterTransientFailures() throws IOException {
        try (StubServer server = new StubServer()) {
            AtomicInteger calls = new AtomicInteger();
            server.route("/v1/self/ready", exchange -> {
                if (calls.incrementAndGet() < 3) {
                    StubServer.respond(exchange, 503, "controller busy");
                } else {
                    StubServer.respond(exchange, 200, READY_JSON);
                }
            });
            GrusClient client = new GrusClient(server.config());
            List<Integer> waits = new ArrayList<>();

            Instance instance = client.readyWithRetry(waits::add, 5);

            assertEquals(3, calls.get(), "ready must be called until the third call succeeds");
            assertEquals(List.of(0, 1), waits, "one backoff wait must precede each retry");
            assertEquals("lobby-1", instance.id(), "the instance of the successful call must be returned");
        }
    }

    @Test
    void readyWithRetryRethrowsLastErrorWhenAllAttemptsFail() throws IOException {
        try (StubServer server = new StubServer()) {
            AtomicInteger calls = new AtomicInteger();
            server.route("/v1/self/ready", exchange -> {
                calls.incrementAndGet();
                StubServer.respond(exchange, 500, "still broken");
            });
            GrusClient client = new GrusClient(server.config());

            GrusException error = assertThrows(GrusException.class,
                    () -> client.readyWithRetry(attempt -> { }, 3),
                    "the error must surface after the last attempt");

            assertEquals(3, calls.get(), "exactly maxAttempts calls must be made");
            assertEquals(500, error.status(), "the status of the last attempt must be kept");
        }
    }

    @Test
    void readyWithRetryRejectsZeroAttempts() throws IOException {
        try (StubServer server = new StubServer()) {
            GrusClient client = new GrusClient(server.config());

            assertThrows(IllegalArgumentException.class, () -> client.readyWithRetry(attempt -> { }, 0),
                    "maxAttempts below 1 must be rejected");
        }
    }
}
