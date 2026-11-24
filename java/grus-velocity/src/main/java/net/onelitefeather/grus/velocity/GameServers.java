package net.onelitefeather.grus.velocity;

import net.onelitefeather.grus.sdk.Instance;
import net.onelitefeather.grus.sdk.InstanceStateChange;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * Keeps the proxy's server list in step with the game instances that Grus reports as Ready.
 *
 * <p>Holds a cache of the game instances and the names this plugin has registered itself. Servers
 * that were not registered here (for example from velocity.toml) are never unregistered. All
 * methods are synchronized: events arrive on the stream thread, fallback choices on Velocity's
 * connection threads.
 */
public final class GameServers {

    static final String ROLE_GAME = "game";
    static final String STATE_READY = "Ready";

    private static final Logger LOG = LoggerFactory.getLogger(GameServers.class);

    private final ProxyRegistry registry;
    private final Map<String, Instance> instances = new HashMap<>();
    /** Names registered by this plugin, mapped to the address text they were registered with. */
    private final Map<String, String> owned = new HashMap<>();

    public GameServers(ProxyRegistry registry) {
        this.registry = registry;
    }

    /** Applies a full instance: from {@code instance.created}/{@code instance.updated} or a full sync. */
    public synchronized void applyInstance(Instance instance) {
        if (!ROLE_GAME.equals(instance.role())) {
            return;
        }
        update(instance);
    }

    /** Applies an {@code instance.state} transition. Only game instances are tracked. */
    public synchronized void applyStateChange(InstanceStateChange change) {
        if (!ROLE_GAME.equals(change.role()) || change.instance() == null) {
            return;
        }
        Instance known = instances.get(change.instance());
        String address = change.address() != null ? change.address() : known != null ? known.address() : null;
        Instance next = known != null
                ? new Instance(known.id(), known.pool(), known.role(), change.to(), address,
                        known.players(), known.maxPlayers(), known.labels())
                : new Instance(change.instance(), change.pool(), change.role(), change.to(), address,
                        0, 0, Map.of());
        update(next);
    }

    /** Drops an instance from the cache and unregisters it when this plugin registered it. */
    public synchronized void remove(String id) {
        instances.remove(id);
        unregister(id);
    }

    /**
     * Replaces the cache with a full listing and reconciles the owned servers against it: every Ready
     * instance is registered, every owned server that is no longer Ready is unregistered.
     */
    public synchronized void sync(List<Instance> listing) {
        instances.clear();
        for (Instance instance : listing) {
            if (ROLE_GAME.equals(instance.role())) {
                instances.put(instance.id(), instance);
            }
        }
        for (String id : new ArrayList<>(owned.keySet())) {
            Instance current = instances.get(id);
            if (current == null || !isReady(current)) {
                unregister(id);
            }
        }
        for (Instance instance : instances.values()) {
            if (isReady(instance)) {
                register(instance.id(), instance.address());
            }
        }
    }

    /**
     * The Ready instance of {@code pool} with the fewest players that still has room and is registered
     * with the proxy. {@code excludedId} is skipped, so a player is not sent back to the server it left.
     */
    public synchronized Optional<Instance> fallbackTarget(String pool, String excludedId) {
        return instances.values().stream()
                .filter(instance -> pool.equals(instance.pool()))
                .filter(instance -> !instance.id().equals(excludedId))
                .filter(GameServers::isReady)
                .filter(instance -> instance.maxPlayers() == 0 || instance.players() < instance.maxPlayers())
                .filter(instance -> registry.hasServer(instance.id()))
                .min(Comparator.comparingInt(Instance::players).thenComparing(Instance::id));
    }

    /** Whether this plugin registered the server with that name. */
    public synchronized boolean isOwned(String id) {
        return owned.containsKey(id);
    }

    private void update(Instance instance) {
        instances.put(instance.id(), instance);
        if (isReady(instance)) {
            register(instance.id(), instance.address());
        } else {
            unregister(instance.id());
        }
    }

    private void register(String id, String address) {
        if (address == null || address.isBlank()) {
            return;
        }
        String current = owned.get(id);
        if (address.equals(current)) {
            return;
        }
        if (current != null) {
            unregister(id);
        }
        HostPort target;
        try {
            target = HostPort.parse(address);
        } catch (IllegalArgumentException e) {
            LOG.warn("Not registering {}: {}", id, e.getMessage());
            return;
        }
        if (registry.hasServer(id)) {
            LOG.debug("Not registering {}: a server with that name already exists", id);
            return;
        }
        registry.registerServer(id, target);
        owned.put(id, address);
        LOG.info("registered {} at {}", id, address);
    }

    private void unregister(String id) {
        if (owned.remove(id) == null) {
            return;
        }
        registry.unregisterServer(id);
        LOG.info("unregistered {}", id);
    }

    private static boolean isReady(Instance instance) {
        return STATE_READY.equals(instance.state()) && instance.address() != null && !instance.address().isBlank();
    }
}
