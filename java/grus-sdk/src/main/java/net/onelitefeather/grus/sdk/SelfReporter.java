package net.onelitefeather.grus.sdk;

import java.util.Map;
import java.util.Objects;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.function.IntSupplier;

/**
 * Sends the player count to the controller ({@code PUT /v1/self/status}) every 5 seconds, and on
 * demand through {@link #trigger()}. The status call doubles as the heartbeat.
 *
 * <p>Failures are logged and swallowed, so a controller outage never affects the game server.
 */
public final class SelfReporter implements AutoCloseable {

    static final long PERIOD_SECONDS = 5;

    private static final System.Logger LOG = System.getLogger(SelfReporter.class.getName());

    private final GrusClient client;
    private final IntSupplier players;
    private final int maxPlayers;
    private final ScheduledExecutorService scheduler;
    private volatile boolean closed;
    private ScheduledFuture<?> periodic;

    public SelfReporter(GrusClient client, IntSupplier players, int maxPlayers, ScheduledExecutorService scheduler) {
        this.client = Objects.requireNonNull(client, "client");
        this.players = Objects.requireNonNull(players, "players");
        this.maxPlayers = maxPlayers;
        this.scheduler = Objects.requireNonNull(scheduler, "scheduler");
    }

    /** Starts the periodic status report, first one after 5 seconds. */
    public synchronized void start() {
        if (closed || periodic != null) {
            return;
        }
        periodic = scheduler.scheduleAtFixedRate(this::sendSafely, PERIOD_SECONDS, PERIOD_SECONDS, TimeUnit.SECONDS);
    }

    /** Queues one status report on the scheduler right away, without waiting for the next period. */
    public void trigger() {
        if (closed) {
            return;
        }
        try {
            scheduler.execute(this::sendSafely);
        } catch (RejectedExecutionException e) {
            LOG.log(System.Logger.Level.DEBUG, "status report dropped, scheduler is shut down");
        }
    }

    /** Sends one status report now on the calling thread. Errors are logged, not thrown. */
    public void sendSafely() {
        if (closed) {
            return;
        }
        try {
            send();
        } catch (RuntimeException e) {
            LOG.log(System.Logger.Level.WARNING, "Grus status report failed: {0}", e.toString());
        }
    }

    /** Sends one status report and lets errors propagate. */
    public void send() {
        client.status(players.getAsInt(), maxPlayers, Map.of());
    }

    @Override
    public synchronized void close() {
        closed = true;
        if (periodic != null) {
            periodic.cancel(false);
            periodic = null;
        }
    }
}
