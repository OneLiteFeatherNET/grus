package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import java.util.Map;

/**
 * A managed server instance as returned by the controller.
 *
 * @param id         instance id, e.g. {@code lobby-7f3k}
 * @param pool       pool the instance belongs to
 * @param role       role, e.g. {@code game} or {@code proxy}
 * @param state      lifecycle state, e.g. {@code Starting}, {@code Ready}, {@code Draining}
 * @param address    {@code host:port} the instance listens on, may be null before it is ready
 * @param players    current player count
 * @param maxPlayers player capacity
 * @param labels     free-form labels reported by the instance
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public record Instance(
        String id,
        String pool,
        String role,
        String state,
        String address,
        int players,
        int maxPlayers,
        Map<String, String> labels) {
}
