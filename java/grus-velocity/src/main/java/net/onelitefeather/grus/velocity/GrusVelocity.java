package net.onelitefeather.grus.velocity;

import com.google.inject.Inject;
import com.fasterxml.jackson.databind.JsonNode;
import com.velocitypowered.api.event.Subscribe;
import com.velocitypowered.api.event.connection.DisconnectEvent;
import com.velocitypowered.api.event.connection.PostLoginEvent;
import com.velocitypowered.api.event.player.KickedFromServerEvent;
import com.velocitypowered.api.event.player.PlayerChooseInitialServerEvent;
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent;
import com.velocitypowered.api.event.proxy.ProxyPingEvent;
import com.velocitypowered.api.event.proxy.ProxyShutdownEvent;
import com.velocitypowered.api.plugin.Plugin;
import com.velocitypowered.api.proxy.ProxyServer;
import com.velocitypowered.api.proxy.server.RegisteredServer;
import net.onelitefeather.grus.sdk.Backoff;
import net.onelitefeather.grus.sdk.EventStream;
import net.onelitefeather.grus.sdk.GrusClient;
import net.onelitefeather.grus.sdk.GrusConfig;
import net.onelitefeather.grus.sdk.GrusEvent;
import net.onelitefeather.grus.sdk.GrusException;
import net.onelitefeather.grus.sdk.Instance;
import net.onelitefeather.grus.sdk.InstanceStateChange;
import net.onelitefeather.grus.sdk.NetworkStatus;
import net.onelitefeather.grus.sdk.SelfReporter;
import org.slf4j.Logger;

import java.util.List;
import java.util.Optional;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;

/**
 * Registers Grus game instances as Velocity servers and sends players to a lobby pool.
 *
 * <p>Configured through the environment the controller injects ({@code GRUS_URL}, {@code GRUS_TOKEN},
 * {@code GRUS_INSTANCE}, {@code GRUS_POOL}, {@code GRUS_ROLE}). Without {@code GRUS_URL} and
 * {@code GRUS_TOKEN} the plugin does nothing. {@code GRUS_FALLBACK_POOL} (default {@code lobby}) names the
 * pool that players fall back to, and {@code GRUS_MAX_PLAYERS} (default 500) is the proxy capacity.
 * {@code GRUS_SHARD_POOL} enables {@code /region <x> <z>} for that sharded pool.
 */
@Plugin(
        id = "grus",
        name = "Grus",
        authors = {"OneLiteFeather"},
        description = "Registers Grus game instances as Velocity servers and falls back to the lobby pool.",
        url = "https://github.com/OneLiteFeatherNET/grus")
public final class GrusVelocity {

    static final String ENV_FALLBACK_POOL = "GRUS_FALLBACK_POOL";
    static final String DEFAULT_FALLBACK_POOL = "lobby";
    /** Pool whose region cells {@code /region} resolves; without it the command is not registered. */
    static final String ENV_SHARD_POOL = "GRUS_SHARD_POOL";
    static final String ENV_MAX_PLAYERS = "GRUS_MAX_PLAYERS";
    static final int DEFAULT_MAX_PLAYERS = 500;
    /** Ready calls before giving up; with the default backoff this covers several minutes of controller outage. */
    static final int READY_ATTEMPTS = 20;

    private final ProxyServer proxy;
    private final Logger logger;
    private volatile Integration integration;

    @Inject
    public GrusVelocity(ProxyServer proxy, Logger logger) {
        this.proxy = proxy;
        this.logger = logger;
    }

    @Subscribe
    public void onInitialize(ProxyInitializeEvent event) {
        Optional<GrusConfig> config = GrusConfig.fromEnv();
        if (config.isEmpty()) {
            logger.info("Grus integration disabled: GRUS_URL or GRUS_TOKEN is not set");
            return;
        }
        GrusClient client = new GrusClient(config.get());
        GameServers servers = new GameServers(new VelocityProxyRegistry(proxy));
        NetworkPlayers network = new NetworkPlayers();
        ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor(
                Thread.ofPlatform().name("grus-self-reporter").daemon(true).factory());
        SelfReporter reporter = new SelfReporter(client, proxy::getPlayerCount, maxPlayersFromEnv(), scheduler);

        // The stream starts first; its onConnected callback runs the full sync once the connection is up.
        EventStream stream = EventStream.start(config.get(),
                List.of("instance.*", NetworkStatus.EVENT_TYPE),
                new Listener(client, servers, network), Backoff.defaults());
        registerRegionCommand(client);
        Thread.ofVirtual().name("grus-ready").start(() -> reportReady(client));
        reporter.start();

        integration = new Integration(servers, network, reporter, stream, scheduler, fallbackPool());
        logger.info("Grus integration enabled for instance {}", config.get().instance());
    }

    @Subscribe
    public void onShutdown(ProxyShutdownEvent event) {
        Integration current = integration;
        integration = null;
        if (current != null) {
            current.close();
        }
    }

    @Subscribe
    public void onPostLogin(PostLoginEvent event) {
        Integration current = integration;
        if (current != null) {
            current.reporter().trigger();
        }
    }

    /** Shows the network-wide count in the server list; the max player count stays as Velocity has it. */
    @Subscribe
    public void onProxyPing(ProxyPingEvent event) {
        Integration current = integration;
        if (current == null) {
            return;
        }
        int shown = NetworkPlayers.shownOnline(current.network().players(), proxy.getPlayerCount());
        event.setPing(event.getPing().asBuilder().onlinePlayers(shown).build());
    }

    @Subscribe
    public void onDisconnect(DisconnectEvent event) {
        Integration current = integration;
        if (current != null) {
            current.reporter().trigger();
        }
    }

    @Subscribe
    public void onChooseInitialServer(PlayerChooseInitialServerEvent event) {
        Integration current = integration;
        if (current == null || event.getInitialServer().isPresent()) {
            return;
        }
        fallback(current, null).ifPresent(event::setInitialServer);
    }

    @Subscribe
    public void onKicked(KickedFromServerEvent event) {
        Integration current = integration;
        if (current == null || event.kickedDuringServerConnect()) {
            return;
        }
        String kickedFrom = event.getServer().getServerInfo().getName();
        fallback(current, kickedFrom).ifPresent(target ->
                event.setResult(KickedFromServerEvent.RedirectPlayer.create(target)));
    }

    private Optional<RegisteredServer> fallback(
            Integration current, String excludedId) {
        return current.servers().fallbackTarget(current.fallbackPool(), excludedId)
                .flatMap(instance -> proxy.getServer(instance.id()));
    }

    private void registerRegionCommand(GrusClient client) {
        String pool = System.getenv(ENV_SHARD_POOL);
        if (pool == null || pool.isBlank()) {
            return;
        }
        var manager = proxy.getCommandManager();
        manager.register(manager.metaBuilder("region").plugin(this).build(),
                new RegionCommand(proxy, new RegionLookup(client, pool.trim()), logger));
        logger.info("Grus /region command enabled for pool {}", pool.trim());
    }

    private void reportReady(GrusClient client) {
        try {
            client.readyWithRetry(Backoff.defaults(), READY_ATTEMPTS);
            logger.info("Grus reported ready");
        } catch (GrusException e) {
            logger.warn("Grus ready call failed with status {}: {}", e.status(), e.getMessage());
        }
    }

    private void fullSync(GrusClient client, GameServers servers, NetworkPlayers network) {
        try {
            servers.sync(client.instances(null, null, GameServers.ROLE_GAME));
        } catch (GrusException e) {
            logger.warn("Grus full sync failed with status {}: {}", e.status(), e.getMessage());
        }
        refreshNetwork(client, network);
    }

    /** Keeps the cached network count when the controller has no endpoint for it (404 leaves it unchanged). */
    private void refreshNetwork(GrusClient client, NetworkPlayers network) {
        try {
            client.network().ifPresent(network::update);
        } catch (GrusException e) {
            logger.warn("Grus network count refresh failed with status {}: {}", e.status(), e.getMessage());
        }
    }

    private static String fallbackPool() {
        String value = System.getenv(ENV_FALLBACK_POOL);
        return value == null || value.isBlank() ? DEFAULT_FALLBACK_POOL : value.trim();
    }

    private int maxPlayersFromEnv() {
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
            // Reported below.
        }
        logger.warn("Ignoring invalid {}, using {}", ENV_MAX_PLAYERS, DEFAULT_MAX_PLAYERS);
        return DEFAULT_MAX_PLAYERS;
    }

    /** Event listener of the stream. Callbacks run on the stream thread, so a full sync blocks only that thread. */
    private final class Listener implements EventStream.Listener {

        private final GrusClient client;
        private final GameServers servers;
        private final NetworkPlayers network;

        Listener(GrusClient client, GameServers servers, NetworkPlayers network) {
            this.client = client;
            this.servers = servers;
            this.network = network;
        }

        @Override
        public void onEvent(GrusEvent event) {
            switch (event.type()) {
                case "instance.state" -> servers.applyStateChange(InstanceStateChange.from(event));
                case "instance.created", "instance.updated" -> servers.applyInstance(Instance.from(event));
                case NetworkStatus.EVENT_TYPE -> network.update(NetworkStatus.from(event));
                case "instance.removed" -> {
                    JsonNode id = event.data().get("instance");
                    if (id != null && id.isTextual()) {
                        servers.remove(id.asText());
                    }
                }
                default -> {
                    // Other instance events do not change the server list.
                }
            }
        }

        @Override
        public void onResync() {
            fullSync(client, servers, network);
        }

        @Override
        public void onConnected() {
            fullSync(client, servers, network);
        }
    }

    /** Everything that exists only while the integration is enabled. */
    private record Integration(
            GameServers servers,
            NetworkPlayers network,
            SelfReporter reporter,
            EventStream stream,
            ScheduledExecutorService scheduler,
            String fallbackPool) {

        void close() {
            stream.close();
            reporter.close();
            scheduler.shutdownNow();
        }
    }
}
