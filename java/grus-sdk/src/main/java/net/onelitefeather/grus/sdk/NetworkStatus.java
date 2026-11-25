package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.core.JsonProcessingException;

/**
 * Network-wide player count as returned by {@code GET /v1/network} and published as the
 * {@value #EVENT_TYPE} event.
 *
 * @param players    players on all Ready or Allocated proxies
 * @param maxPlayers capacity of those proxies
 * @param proxies    number of proxies counted
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public record NetworkStatus(int players, int maxPlayers, int proxies) {

    /** SSE event name; subscribe with {@code EventStream.start(..., List.of(NetworkStatus.EVENT_TYPE), ...)}. */
    public static final String EVENT_TYPE = "network.players";

    /**
     * Parses the data of a {@value #EVENT_TYPE} event.
     *
     * @throws IllegalArgumentException when the payload does not match this shape
     */
    public static NetworkStatus from(GrusEvent event) {
        try {
            return Json.MAPPER.treeToValue(event.data(), NetworkStatus.class);
        } catch (JsonProcessingException | IllegalArgumentException e) {
            throw new IllegalArgumentException("not a network status payload: " + event.type(), e);
        }
    }
}
