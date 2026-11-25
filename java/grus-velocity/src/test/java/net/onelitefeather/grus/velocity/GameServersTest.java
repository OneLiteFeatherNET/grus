package net.onelitefeather.grus.velocity;

import net.onelitefeather.grus.sdk.Instance;
import net.onelitefeather.grus.sdk.InstanceStateChange;
import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.Map;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

class GameServersTest {

    private static final String ADDRESS = "10.0.12.34:25565";

    @Test
    void readyStateRegistersServerWithParsedAddress() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);

        servers.applyStateChange(change("lobby-1", "Starting", "Ready", ADDRESS));

        assertEquals(List.of("register lobby-1"), proxy.calls(), "a Ready instance must be registered once");
        assertEquals(new HostPort("10.0.12.34", 25565), proxy.address("lobby-1"), "the address must be parsed");
        assertTrue(servers.isOwned("lobby-1"), "the plugin must remember that it registered the server");
    }

    @Test
    void readyStateWithoutAddressDoesNotRegister() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);

        servers.applyStateChange(change("lobby-1", "Starting", "Ready", null));

        assertEquals(List.of(), proxy.calls(), "without an address the server cannot be routed to");
    }

    @Test
    void serverWithExistingNameIsNotTouched() {
        FakeProxyRegistry proxy = new FakeProxyRegistry()
                .withStaticServer("lobby-1", new HostPort("127.0.0.1", 30000));
        GameServers servers = new GameServers(proxy);

        servers.applyStateChange(change("lobby-1", "Starting", "Ready", ADDRESS));
        servers.applyStateChange(change("lobby-1", "Ready", "Stopped", null));

        assertEquals(List.of(), proxy.calls(), "a server from velocity.toml must never be registered or removed");
        assertEquals(new HostPort("127.0.0.1", 30000), proxy.address("lobby-1"), "the static server must stay as it was");
    }

    @Test
    void newlyRegisteredServerIsPingedOnce() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);

        servers.applyStateChange(change("lobby-1", "Starting", "Ready", ADDRESS));
        servers.applyStateChange(change("lobby-1", "Ready", "Ready", ADDRESS));

        assertEquals(List.of("lobby-1"), proxy.pings(),
                "the backend must be pinged once after registration, not on every update");
    }

    @Test
    void serverThatIsNotRegisteredIsNotPinged() {
        FakeProxyRegistry proxy = new FakeProxyRegistry()
                .withStaticServer("lobby-1", new HostPort("127.0.0.1", 30000));
        GameServers servers = new GameServers(proxy);

        servers.applyStateChange(change("lobby-1", "Starting", "Ready", ADDRESS));

        assertEquals(List.of(), proxy.pings(), "a server that was not registered by Grus must not be pinged");
    }

    @Test
    void drainingStateUnregistersOwnedServer() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.applyStateChange(change("lobby-1", "Starting", "Ready", ADDRESS));

        servers.applyStateChange(change("lobby-1", "Ready", "Draining", null));

        assertEquals(List.of("register lobby-1", "unregister lobby-1"), proxy.calls(),
                "a draining instance must be taken out of the server list");
        assertFalse(servers.isOwned("lobby-1"), "the plugin must forget the server after unregistering it");
    }

    @Test
    void stateChangeOfOtherRoleIsIgnored() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);

        servers.applyStateChange(new InstanceStateChange("proxy-1", "proxy", "proxy", "Starting", "Ready", ADDRESS, null));

        assertEquals(List.of(), proxy.calls(), "only game instances are registered by this plugin");
    }

    @Test
    void removedInstanceIsUnregisteredAndDropped() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.applyStateChange(change("lobby-1", "Starting", "Ready", ADDRESS));

        servers.remove("lobby-1");

        assertEquals(List.of("register lobby-1", "unregister lobby-1"), proxy.calls(),
                "a removed instance must leave the server list");
    }

    @Test
    void syncRegistersReadyAndUnregistersOwnedServersNoLongerReady() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.applyStateChange(change("old", "Starting", "Ready", "10.0.0.1:25565"));

        servers.sync(List.of(
                instance("fresh", "lobby", "Ready", "10.0.0.2:25565", 0, 20),
                instance("busy", "lobby", "Stopping", "10.0.0.3:25565", 0, 20)));

        assertEquals(List.of("register old", "unregister old", "register fresh"), proxy.calls(),
                "sync must register Ready servers and unregister owned ones that are gone");
        assertFalse(servers.isOwned("old"), "a server missing from the listing must not stay owned");
        assertTrue(servers.isOwned("fresh"), "a Ready server from the listing must be owned");
    }

    @Test
    void fallbackPicksReadyInstanceWithFewestPlayers() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.sync(List.of(
                instance("lobby-a", "lobby", "Ready", "10.0.0.1:25565", 12, 20),
                instance("lobby-b", "lobby", "Ready", "10.0.0.2:25565", 3, 20),
                instance("game-1", "minigame", "Ready", "10.0.0.3:25565", 0, 20)));

        Optional<Instance> target = servers.fallbackTarget("lobby", null);

        assertEquals("lobby-b", target.map(Instance::id).orElse(null), "the lobby with the fewest players must win");
    }

    @Test
    void fallbackSkipsFullInstances() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.sync(List.of(
                instance("lobby-full", "lobby", "Ready", "10.0.0.1:25565", 20, 20),
                instance("lobby-open", "lobby", "Ready", "10.0.0.2:25565", 19, 20)));

        assertEquals("lobby-open", servers.fallbackTarget("lobby", null).map(Instance::id).orElse(null),
                "an instance at capacity must not receive players");
    }

    @Test
    void fallbackTreatsZeroCapacityAsUnlimited() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.sync(List.of(instance("lobby-1", "lobby", "Ready", "10.0.0.1:25565", 400, 0)));

        assertEquals("lobby-1", servers.fallbackTarget("lobby", null).map(Instance::id).orElse(null),
                "maxPlayers 0 means no capacity limit");
    }

    @Test
    void fallbackSkipsExcludedInstance() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.sync(List.of(
                instance("lobby-a", "lobby", "Ready", "10.0.0.1:25565", 1, 20),
                instance("lobby-b", "lobby", "Ready", "10.0.0.2:25565", 5, 20)));

        assertEquals("lobby-b", servers.fallbackTarget("lobby", "lobby-a").map(Instance::id).orElse(null),
                "the server the player was kicked from must be skipped");
    }

    @Test
    void fallbackSkipsInstancesThatAreNotReady() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.sync(List.of(instance("lobby-1", "lobby", "Ready", "10.0.0.1:25565", 0, 20)));
        servers.applyStateChange(change("lobby-1", "Ready", "Draining", null));

        assertEquals(Optional.empty(), servers.fallbackTarget("lobby", null),
                "a draining instance must not be a fallback target");
    }

    @Test
    void fallbackReturnsEmptyForUnknownPool() {
        GameServers servers = new GameServers(new FakeProxyRegistry());

        assertEquals(Optional.empty(), servers.fallbackTarget("lobby", null),
                "without any lobby instance there is no fallback");
    }

    @Test
    void updatedInstanceRefreshesPlayerCountWithoutRegisteringTwice() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.applyStateChange(change("lobby-1", "Starting", "Ready", ADDRESS));

        servers.applyInstance(instance("lobby-1", "lobby", "Ready", ADDRESS, 7, 20));

        assertEquals(List.of("register lobby-1"), proxy.calls(), "an update must not re-register the server");
        assertEquals(7, servers.fallbackTarget("lobby", null).map(Instance::players).orElse(-1),
                "the update must carry the new player count into the cache");
    }

    @Test
    void stateChangeKeepsPlayerCountFromCache() {
        FakeProxyRegistry proxy = new FakeProxyRegistry();
        GameServers servers = new GameServers(proxy);
        servers.applyInstance(instance("lobby-1", "lobby", "Ready", ADDRESS, 4, 20));

        servers.applyStateChange(change("lobby-1", "Ready", "Ready", ADDRESS));

        assertEquals(4, servers.fallbackTarget("lobby", null).map(Instance::players).orElse(-1),
                "a state change must not reset the player count");
    }

    private static InstanceStateChange change(String id, String from, String to, String address) {
        return new InstanceStateChange(id, "lobby", "game", from, to, address, null);
    }

    private static Instance instance(String id, String pool, String state, String address, int players, int max) {
        return new Instance(id, pool, "game", state, address, players, max, Map.of());
    }
}
