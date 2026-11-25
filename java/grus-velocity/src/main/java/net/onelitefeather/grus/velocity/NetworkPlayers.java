package net.onelitefeather.grus.velocity;

import net.onelitefeather.grus.sdk.NetworkStatus;

/**
 * Last known network-wide player count. Written from the event stream and the initial GET, read from
 * Velocity's ping handler, so it is volatile rather than synchronized.
 */
final class NetworkPlayers {

    private volatile int players;

    /** The online count to show in the server list: the network total, never less than this proxy's own players. */
    static int shownOnline(int networkPlayers, int localPlayers) {
        return Math.max(networkPlayers, localPlayers);
    }

    void update(NetworkStatus status) {
        players = status.players();
    }

    int players() {
        return players;
    }
}
