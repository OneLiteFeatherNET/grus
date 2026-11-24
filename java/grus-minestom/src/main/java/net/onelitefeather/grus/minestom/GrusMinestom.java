package net.onelitefeather.grus.minestom;

import net.minestom.server.MinecraftServer;
import net.minestom.server.event.GlobalEventHandler;
import net.minestom.server.event.player.PlayerDisconnectEvent;
import net.minestom.server.event.player.PlayerSpawnEvent;
import net.onelitefeather.grus.sdk.Backoff;
import net.onelitefeather.grus.sdk.GrusClient;
import net.onelitefeather.grus.sdk.GrusConfig;
import net.onelitefeather.grus.sdk.GrusException;
import net.onelitefeather.grus.sdk.SelfReporter;

import java.util.Optional;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.function.IntSupplier;

/**
 * Connects a Minestom server to Grus: reports readiness, sends the player count every 5 seconds
 * and immediately on join and quit.
 *
 * <p>Configured through the environment the controller injects ({@code GRUS_URL}, {@code GRUS_TOKEN},
 * {@code GRUS_INSTANCE}, {@code GRUS_POOL}, {@code GRUS_ROLE}) plus {@code GRUS_MAX_PLAYERS}
 * (default 20). Without {@code GRUS_URL} and {@code GRUS_TOKEN} the integration stays disabled.
 */
public final class GrusMinestom {

    /** Environment variable with the player capacity reported to Grus. */
    public static final String ENV_MAX_PLAYERS = "GRUS_MAX_PLAYERS";
    static final int DEFAULT_MAX_PLAYERS = 20;
    /** Ready calls before giving up; with the default backoff this covers several minutes of controller outage. */
    static final int READY_ATTEMPTS = 20;

    private static final System.Logger LOG = System.getLogger(GrusMinestom.class.getName());

    private final GrusClient client;
    private final SelfReporter reporter;
    private final ScheduledExecutorService scheduler;

    private GrusMinestom(GrusClient client, SelfReporter reporter, ScheduledExecutorService scheduler) {
        this.client = client;
        this.reporter = reporter;
        this.scheduler = scheduler;
    }

    /**
     * Creates the integration from the environment and registers the player listeners.
     * Call {@link #ready()} once the server accepts connections.
     *
     * @return the integration, or empty when Grus is not configured
     */
    public static Optional<GrusMinestom> install() {
        Optional<GrusConfig> config = GrusConfig.fromEnv();
        if (config.isEmpty()) {
            LOG.log(System.Logger.Level.INFO, "Grus integration disabled: GRUS_URL or GRUS_TOKEN is not set");
            return Optional.empty();
        }
        int maxPlayers = maxPlayersFromEnv();
        GrusClient client = new GrusClient(config.get());
        ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor(
                Thread.ofPlatform().name("grus-self-reporter").daemon(true).factory());
        IntSupplier players = () -> MinecraftServer.getConnectionManager().getOnlinePlayers().size();
        SelfReporter reporter = new SelfReporter(client, players, maxPlayers, scheduler);
        GrusMinestom grus = new GrusMinestom(client, reporter, scheduler);
        grus.registerPlayerListeners();
        LOG.log(System.Logger.Level.INFO, "Grus integration enabled for instance {0}", config.get().instance());
        return Optional.of(grus);
    }

    /**
     * Reports this instance as ready and starts the periodic status reports.
     * The ready call is retried in the background on a virtual thread, so a transient controller error
     * does not leave the instance stuck in Starting. The status reports start right away.
     */
    public void ready() {
        Thread.ofVirtual().name("grus-ready").start(this::reportReadyWithRetry);
        reporter.start();
    }

    private void reportReadyWithRetry() {
        try {
            client.readyWithRetry(Backoff.defaults(), READY_ATTEMPTS);
            LOG.log(System.Logger.Level.INFO, "Grus reported ready");
        } catch (GrusException e) {
            LOG.log(System.Logger.Level.WARNING, "Grus ready call failed with status {0}: {1}", e.status(), e.getMessage());
        }
    }

    /** Stops the status reports. The server itself is not touched. */
    public void close() {
        reporter.close();
        scheduler.shutdownNow();
    }

    private void registerPlayerListeners() {
        GlobalEventHandler handler = MinecraftServer.getGlobalEventHandler();
        handler.addListener(PlayerSpawnEvent.class, event -> {
            if (event.isFirstSpawn()) {
                reporter.trigger();
            }
        });
        handler.addListener(PlayerDisconnectEvent.class, event -> reporter.trigger());
    }

    static int maxPlayersFromEnv() {
        String raw = System.getenv(ENV_MAX_PLAYERS);
        if (raw == null || raw.isBlank()) {
            return DEFAULT_MAX_PLAYERS;
        }
        try {
            int value = Integer.parseInt(raw.trim());
            if (value > 0) {
                return value;
            }
        } catch (NumberFormatException ignored) {
            // Handled below with a warning.
        }
        LOG.log(System.Logger.Level.WARNING, "Ignoring invalid {0}, using {1}", ENV_MAX_PLAYERS, DEFAULT_MAX_PLAYERS);
        return DEFAULT_MAX_PLAYERS;
    }
}
