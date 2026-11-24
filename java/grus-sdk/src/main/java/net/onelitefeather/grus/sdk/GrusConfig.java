package net.onelitefeather.grus.sdk;

import java.net.URI;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;

/**
 * Connection settings for a Grus controller, injected by the controller through environment variables.
 *
 * @param url      base URL of the controller, e.g. {@code http://grus.grus-dev.svc:8080}
 * @param token    bearer token of this instance (or an admin token in development)
 * @param instance instance id ({@code GRUS_INSTANCE}), may be null outside a Grus-managed server
 * @param pool     pool name ({@code GRUS_POOL}), may be null
 * @param role     role such as {@code game} or {@code proxy} ({@code GRUS_ROLE}), may be null
 */
public record GrusConfig(URI url, String token, String instance, String pool, String role) {

    public static final String ENV_URL = "GRUS_URL";
    public static final String ENV_TOKEN = "GRUS_TOKEN";
    public static final String ENV_INSTANCE = "GRUS_INSTANCE";
    public static final String ENV_POOL = "GRUS_POOL";
    public static final String ENV_ROLE = "GRUS_ROLE";

    public GrusConfig {
        Objects.requireNonNull(url, "url");
        Objects.requireNonNull(token, "token");
    }

    /**
     * Reads the configuration from the process environment.
     *
     * @return the configuration, or empty when {@code GRUS_URL} or {@code GRUS_TOKEN} is missing or blank
     */
    public static Optional<GrusConfig> fromEnv() {
        Map<String, String> env = System.getenv();
        if (isBlank(env.get(ENV_URL)) || isBlank(env.get(ENV_TOKEN))) {
            return Optional.empty();
        }
        return Optional.of(fromMap(env));
    }

    /**
     * Builds the configuration from a map with the {@code GRUS_*} keys (used by tests).
     *
     * @throws IllegalArgumentException when {@code GRUS_URL} or {@code GRUS_TOKEN} is missing or blank
     */
    public static GrusConfig fromMap(Map<String, String> values) {
        String url = values.get(ENV_URL);
        String token = values.get(ENV_TOKEN);
        if (isBlank(url) || isBlank(token)) {
            throw new IllegalArgumentException(ENV_URL + " and " + ENV_TOKEN + " are required");
        }
        return new GrusConfig(
                URI.create(url.trim()),
                token.trim(),
                blankToNull(values.get(ENV_INSTANCE)),
                blankToNull(values.get(ENV_POOL)),
                blankToNull(values.get(ENV_ROLE)));
    }

    /** The token is masked so that logging a configuration never leaks it. */
    @Override
    public String toString() {
        return "GrusConfig[url=" + url + ", token=***, instance=" + instance
                + ", pool=" + pool + ", role=" + role + "]";
    }

    private static boolean isBlank(String value) {
        return value == null || value.isBlank();
    }

    private static String blankToNull(String value) {
        return isBlank(value) ? null : value.trim();
    }
}
