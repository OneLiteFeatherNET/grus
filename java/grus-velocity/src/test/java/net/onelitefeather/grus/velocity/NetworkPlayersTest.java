package net.onelitefeather.grus.velocity;

import net.onelitefeather.grus.sdk.NetworkStatus;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

class NetworkPlayersTest {

    @Test
    void shownOnlineIsNetworkCountWhenItIsLargerThanLocal() {
        assertEquals(42, NetworkPlayers.shownOnline(42, 5), "the ping must show the whole network");
    }

    @Test
    void shownOnlineIsLocalCountWhenNetworkCountLagsBehind() {
        assertEquals(25, NetworkPlayers.shownOnline(10, 25),
                "players who joined after the last network update must still be counted");
    }

    @Test
    void shownOnlineFallsBackToLocalCountWhenNetworkIsUnknown() {
        assertEquals(7, NetworkPlayers.shownOnline(0, 7),
                "without a network count (404 or not yet fetched) the local count is shown");
    }

    @Test
    void updateReplacesTheCachedNetworkCount() {
        NetworkPlayers cache = new NetworkPlayers();
        cache.update(new NetworkStatus(42, 1000, 3));
        cache.update(new NetworkStatus(17, 1000, 3));

        assertEquals(17, cache.players(), "the latest network.players event wins");
    }

    @Test
    void cacheStartsAtZero() {
        assertEquals(0, new NetworkPlayers().players(), "nothing is known before the first GET or event");
    }
}
