package net.onelitefeather.grus.sdk;

import com.sun.net.httpserver.HttpExchange;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

class EventStreamTest {

    private static final long WAIT_SECONDS = 5;
    private static final Backoff IMMEDIATE = attempt -> {
    };

    @Test
    void reconnectsAfterServerClosesAndResumesWithLastEventId() throws Exception {
        try (StubServer server = new StubServer()) {
            CountDownLatch release = new CountDownLatch(1);
            BlockingQueue<String> resumeHeaders = new LinkedBlockingQueue<>();
            AtomicInteger connections = new AtomicInteger();
            server.route("/v1/events", exchange -> {
                resumeHeaders.add(StubServer.header(exchange, "Last-Event-ID"));
                if (connections.incrementAndGet() == 1) {
                    // First connection: one event, then the server ends the stream.
                    startStream(exchange);
                    write(exchange, "id: 1\nevent: instance.state\ndata: {\"instance\":\"lobby-1\",\"to\":\"Ready\"}\n\n");
                    exchange.getResponseBody().close();
                } else {
                    // Reconnect: one more event, then keep the stream open until the test releases it.
                    startStream(exchange);
                    write(exchange, "id: 2\nevent: instance.updated\ndata: {\"instance\":\"lobby-1\"}\n\n");
                    awaitQuietly(release);
                    exchange.getResponseBody().close();
                }
            });
            BlockingQueue<GrusEvent> events = new LinkedBlockingQueue<>();
            EventStream.Listener listener = new EventStream.Listener() {
                @Override
                public void onEvent(GrusEvent event) {
                    events.add(event);
                }
            };

            EventStream stream = EventStream.start(server.config(), List.of("instance.*"), listener, IMMEDIATE);
            try {
                GrusEvent first = events.poll(WAIT_SECONDS, TimeUnit.SECONDS);
                GrusEvent second = events.poll(WAIT_SECONDS, TimeUnit.SECONDS);

                assertNotNull(first, "the first event must be delivered");
                assertEquals("1", first.id(), "the first event id must be kept");
                assertNotNull(second, "the event after the reconnect must be delivered");
                assertEquals("2", second.id(), "the event after the reconnect must be the second one");
                assertEquals("", resumeHeaders.poll(WAIT_SECONDS, TimeUnit.SECONDS),
                        "the first connect must not send Last-Event-ID");
                assertEquals("1", resumeHeaders.poll(WAIT_SECONDS, TimeUnit.SECONDS),
                        "the reconnect must send Last-Event-ID of the last delivered event");
            } finally {
                stream.close();
                release.countDown();
            }
        }
    }

    @Test
    void resyncEventCallsOnResyncInsteadOfOnEvent() throws Exception {
        try (StubServer server = new StubServer()) {
            CountDownLatch release = new CountDownLatch(1);
            server.route("/v1/events", exchange -> {
                startStream(exchange);
                write(exchange, "id: 9\nevent: resync\ndata: {}\n\n");
                awaitQuietly(release);
                exchange.getResponseBody().close();
            });
            CountDownLatch resynced = new CountDownLatch(1);
            BlockingQueue<GrusEvent> events = new LinkedBlockingQueue<>();
            EventStream.Listener listener = new EventStream.Listener() {
                @Override
                public void onEvent(GrusEvent event) {
                    events.add(event);
                }

                @Override
                public void onResync() {
                    resynced.countDown();
                }
            };

            EventStream stream = EventStream.start(server.config(), List.of("resync"), listener, IMMEDIATE);
            try {
                assertTrue(resynced.await(WAIT_SECONDS, TimeUnit.SECONDS), "resync must call onResync");
                assertTrue(events.isEmpty(), "resync must not be delivered as a regular event");
            } finally {
                stream.close();
                release.countDown();
            }
        }
    }

    private static void startStream(HttpExchange exchange) throws IOException {
        exchange.getResponseHeaders().add("Content-Type", "text/event-stream");
        exchange.sendResponseHeaders(200, 0);
    }

    private static void write(HttpExchange exchange, String text) throws IOException {
        OutputStream out = exchange.getResponseBody();
        out.write(text.getBytes(StandardCharsets.UTF_8));
        out.flush();
    }

    private static void awaitQuietly(CountDownLatch latch) {
        try {
            latch.await(10, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
