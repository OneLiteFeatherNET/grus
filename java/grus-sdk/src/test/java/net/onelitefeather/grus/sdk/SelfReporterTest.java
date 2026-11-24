package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.lang.reflect.Proxy;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

class SelfReporterTest {

    private static final long WAIT_SECONDS = 5;

    @Test
    void sendReportsCurrentPlayerCountAndMaxPlayers() throws Exception {
        try (StubServer server = new StubServer()) {
            AtomicReference<String> body = new AtomicReference<>();
            server.route("/v1/self/status", exchange -> {
                body.set(StubServer.readBody(exchange));
                StubServer.respond(exchange, 200, "{}");
            });
            SelfReporter reporter = new SelfReporter(new GrusClient(server.config()), () -> 7, 20,
                    Executors.newSingleThreadScheduledExecutor());
            try {
                reporter.send();

                var sent = new ObjectMapper().readTree(body.get());
                assertEquals(7, sent.get("players").asInt(), "the player count must come from the supplier");
                assertEquals(20, sent.get("maxPlayers").asInt(), "maxPlayers must be sent");
            } finally {
                reporter.close();
            }
        }
    }

    @Test
    void failingReportIsSwallowedBySendSafely() throws Exception {
        try (StubServer server = new StubServer()) {
            AtomicInteger calls = new AtomicInteger();
            server.route("/v1/self/status", exchange -> {
                calls.incrementAndGet();
                StubServer.respond(exchange, 500, "boom");
            });
            SelfReporter reporter = new SelfReporter(new GrusClient(server.config()), () -> 0, 20,
                    Executors.newSingleThreadScheduledExecutor());
            try {
                assertDoesNotThrow(reporter::sendSafely, "a controller error must not reach the game server");
                assertEquals(1, calls.get(), "the report must still have been attempted");
            } finally {
                reporter.close();
            }
        }
    }

    @Test
    void startSchedulesAStatusReportEveryFiveSeconds() throws Exception {
        try (StubServer server = new StubServer()) {
            AtomicInteger calls = new AtomicInteger();
            server.route("/v1/self/status", exchange -> {
                calls.incrementAndGet();
                StubServer.respond(exchange, 200, "{}");
            });
            RecordingScheduler scheduler = new RecordingScheduler();
            SelfReporter reporter = new SelfReporter(new GrusClient(server.config()), () -> 0, 20, scheduler.proxy());

            reporter.start();

            assertEquals(5L, scheduler.period, "the status must be reported with a 5 second period");
            assertEquals(TimeUnit.SECONDS, scheduler.unit, "the period must be in seconds");
            scheduler.task.run();
            assertEquals(1, calls.get(), "the scheduled task must send a status report");
            reporter.close();
        }
    }

    @Test
    void triggerSendsOnTheSchedulerWithoutWaitingForThePeriod() throws Exception {
        try (StubServer server = new StubServer()) {
            CountDownLatch received = new CountDownLatch(1);
            server.route("/v1/self/status", exchange -> {
                StubServer.respond(exchange, 200, "{}");
                received.countDown();
            });
            ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor();
            SelfReporter reporter = new SelfReporter(new GrusClient(server.config()), () -> 1, 20, scheduler);
            try {
                reporter.trigger();

                assertTrue(received.await(WAIT_SECONDS, TimeUnit.SECONDS), "trigger must send a status report");
            } finally {
                reporter.close();
                scheduler.shutdownNow();
            }
        }
    }

    /** Captures the periodic task instead of running it, so the test controls when it fires. */
    private static final class RecordingScheduler {

        private long period;
        private TimeUnit unit;
        private Runnable task;

        ScheduledExecutorService proxy() {
            return (ScheduledExecutorService) Proxy.newProxyInstance(
                    ScheduledExecutorService.class.getClassLoader(),
                    new Class<?>[]{ScheduledExecutorService.class},
                    (proxy, method, args) -> {
                        if ("scheduleAtFixedRate".equals(method.getName())) {
                            task = (Runnable) args[0];
                            period = (Long) args[2];
                            unit = (TimeUnit) args[3];
                            return null;
                        }
                        throw new UnsupportedOperationException(method.getName());
                    });
        }
    }
}
