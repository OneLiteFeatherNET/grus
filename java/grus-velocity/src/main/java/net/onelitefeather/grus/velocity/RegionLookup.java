package net.onelitefeather.grus.velocity;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import net.onelitefeather.grus.sdk.GrusClient;
import net.onelitefeather.grus.sdk.GrusException;

import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;

/**
 * Asks the controller which instance serves a block position of a sharded pool
 * ({@code GET /v1/pools/{pool}/cells/at?x=&z=}).
 */
public final class RegionLookup {

    private static final ObjectMapper MAPPER = new ObjectMapper()
            .configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false);

    /** The cell that owns a position and the instance that serves it. */
    @JsonIgnoreProperties(ignoreUnknown = true)
    public record Owner(String cell, String instance, String address) {
    }

    private final GrusClient client;
    private final String pool;

    public RegionLookup(GrusClient client, String pool) {
        this.client = Objects.requireNonNull(client, "client");
        this.pool = Objects.requireNonNull(pool, "pool");
    }

    /**
     * Resolves block coordinates.
     *
     * @return the owner, or empty when no cell covers the position (404) or its cell has no Ready instance yet (409)
     * @throws GrusException on any other controller error
     */
    public Optional<Owner> find(int x, int z) {
        try {
            return Optional.of(parse(client.get(path(pool), query(x, z))));
        } catch (GrusException e) {
            if (e.status() == 404 || e.status() == 409) {
                return Optional.empty();
            }
            throw e;
        }
    }

    /** Path of the cell lookup of a pool; the pool name is percent-encoded. */
    static String path(String pool) {
        return "/v1/pools/" + URLEncoder.encode(pool, StandardCharsets.UTF_8).replace("+", "%20") + "/cells/at";
    }

    /** Query parameters of the lookup. Coordinates are block coordinates; the controller converts to chunks. */
    static Map<String, String> query(int x, int z) {
        return Map.of("x", Integer.toString(x), "z", Integer.toString(z));
    }

    /**
     * Parses the answer of the lookup.
     *
     * @throws IllegalArgumentException when the body is not a cell with an instance
     */
    static Owner parse(String body) {
        Owner owner;
        try {
            owner = MAPPER.readValue(body, Owner.class);
        } catch (JsonProcessingException e) {
            throw new IllegalArgumentException("not a cell answer: " + e.getOriginalMessage(), e);
        }
        if (owner == null || owner.instance() == null || owner.instance().isBlank()) {
            throw new IllegalArgumentException("cell answer has no instance");
        }
        return owner;
    }
}
