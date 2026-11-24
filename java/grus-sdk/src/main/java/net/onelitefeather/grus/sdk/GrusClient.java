package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.core.type.TypeReference;

import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/**
 * Blocking client for the Grus REST API ({@code /v1}). Thread-safe.
 *
 * <p>Non-2xx responses and transport errors are reported as {@link GrusException}.
 */
public final class GrusClient {

    private static final Duration TIMEOUT = Duration.ofSeconds(10);

    private final String baseUrl;
    private final String authorization;
    private final HttpClient http;

    public GrusClient(GrusConfig config) {
        this.baseUrl = stripTrailingSlash(config.url().toString());
        this.authorization = "Bearer " + config.token();
        this.http = HttpClient.newBuilder()
                .connectTimeout(TIMEOUT)
                .build();
    }

    /**
     * Lists instances. {@code null} filters are omitted.
     */
    public List<Instance> instances(String pool, String state, String role) {
        Map<String, String> query = new LinkedHashMap<>();
        putIfPresent(query, "pool", pool);
        putIfPresent(query, "state", state);
        putIfPresent(query, "role", role);
        String body = send("GET", "/v1/instances", query, null);
        return read(body, new TypeReference<List<Instance>>() {
        });
    }

    public Instance instance(String id) {
        Objects.requireNonNull(id, "id");
        return read(send("GET", "/v1/instances/" + encode(id), Map.of(), null), Instance.class);
    }

    public Instance self() {
        return read(send("GET", "/v1/self", Map.of(), null), Instance.class);
    }

    /** Reports this instance as ready ({@code POST /v1/self/ready}). */
    public Instance ready() {
        return read(send("POST", "/v1/self/ready", Map.of(), null), Instance.class);
    }

    /**
     * Calls {@link #ready()} until the controller accepts it, waiting {@code backoff} between attempts.
     * Any {@link GrusException} (transport error or non-2xx) counts as a failed attempt.
     *
     * @param maxAttempts total number of calls, at least 1
     * @return the instance as reported by the controller on the successful call
     * @throws GrusException the error of the last attempt when all attempts failed
     */
    public Instance readyWithRetry(Backoff backoff, int maxAttempts) {
        Objects.requireNonNull(backoff, "backoff");
        if (maxAttempts < 1) {
            throw new IllegalArgumentException("maxAttempts must be at least 1: " + maxAttempts);
        }
        for (int attempt = 0; ; attempt++) {
            try {
                return ready();
            } catch (GrusException e) {
                if (attempt + 1 >= maxAttempts) {
                    throw e;
                }
            }
            try {
                backoff.sleep(attempt);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new GrusException("Grus ready retry interrupted", 0, "", e);
            }
        }
    }

    /** Reports players and labels ({@code PUT /v1/self/status}); counts as a heartbeat. */
    public Instance status(int players, int maxPlayers, Map<String, String> labels) {
        Map<String, Object> body = new LinkedHashMap<>();
        body.put("players", players);
        body.put("maxPlayers", maxPlayers);
        body.put("labels", labels == null ? Map.of() : labels);
        return read(send("PUT", "/v1/self/status", Map.of(), toJson(body)), Instance.class);
    }

    /** Marks this instance as allocated to a player group ({@code POST /v1/self/allocate}). */
    public Instance allocate() {
        return read(send("POST", "/v1/self/allocate", Map.of(), null), Instance.class);
    }

    /** Asks the controller to shut this instance down ({@code POST /v1/self/shutdown}). */
    public Instance shutdown() {
        return read(send("POST", "/v1/self/shutdown", Map.of(), null), Instance.class);
    }

    private String send(String method, String path, Map<String, String> query, byte[] json) {
        HttpRequest.BodyPublisher publisher = json == null
                ? HttpRequest.BodyPublishers.noBody()
                : HttpRequest.BodyPublishers.ofByteArray(json);
        HttpRequest.Builder request = HttpRequest.newBuilder(uri(path, query))
                .timeout(TIMEOUT)
                .header("Authorization", authorization)
                .header("Accept", "application/json")
                .method(method, publisher);
        if (json != null) {
            request.header("Content-Type", "application/json");
        }
        HttpResponse<String> response;
        try {
            response = http.send(request.build(), HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8));
        } catch (IOException e) {
            throw new GrusException("Grus request " + method + " " + path + " failed", 0, "", e);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new GrusException("Grus request " + method + " " + path + " interrupted", 0, "", e);
        }
        int status = response.statusCode();
        if (status < 200 || status >= 300) {
            throw new GrusException("Grus request " + method + " " + path + " returned HTTP " + status,
                    status, response.body());
        }
        return response.body();
    }

    private URI uri(String path, Map<String, String> query) {
        StringBuilder url = new StringBuilder(baseUrl).append(path);
        char separator = '?';
        for (Map.Entry<String, String> entry : query.entrySet()) {
            url.append(separator).append(encode(entry.getKey())).append('=').append(encode(entry.getValue()));
            separator = '&';
        }
        return URI.create(url.toString());
    }

    private static <T> T read(String body, Class<T> type) {
        try {
            return Json.MAPPER.readValue(body, type);
        } catch (JsonProcessingException e) {
            throw new GrusException("Grus response is not valid JSON for " + type.getSimpleName(), 200, body, e);
        }
    }

    private static <T> T read(String body, TypeReference<T> type) {
        try {
            return Json.MAPPER.readValue(body, type);
        } catch (JsonProcessingException e) {
            throw new GrusException("Grus response is not a valid JSON array", 200, body, e);
        }
    }

    private static byte[] toJson(Object value) {
        try {
            return Json.MAPPER.writeValueAsBytes(value);
        } catch (JsonProcessingException e) {
            throw new IllegalStateException("cannot serialize request body", e);
        }
    }

    private static void putIfPresent(Map<String, String> query, String key, String value) {
        if (value != null) {
            query.put(key, value);
        }
    }

    private static String encode(String value) {
        return URLEncoder.encode(value, StandardCharsets.UTF_8).replace("+", "%20");
    }

    private static String stripTrailingSlash(String url) {
        return url.endsWith("/") ? url.substring(0, url.length() - 1) : url;
    }
}
