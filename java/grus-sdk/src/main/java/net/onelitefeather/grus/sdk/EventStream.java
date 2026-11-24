package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Collection;
import java.util.Objects;
import java.util.stream.Collectors;

/**
 * Subscription to {@code GET /v1/events} on a virtual thread.
 *
 * <p>Connects with {@code Accept: text/event-stream}; after the first event it sends
 * {@code Last-Event-ID} on every reconnect. On a disconnect or error it waits according to
 * {@link Backoff} and reconnects. A {@code resync} event is not delivered as an event but as
 * {@link Listener#onResync()}, which tells the consumer to reload its full state.
 */
public final class EventStream implements AutoCloseable {

    /** Callbacks run on the stream's thread. Exceptions thrown by them are logged and swallowed. */
    public interface Listener {

        void onEvent(GrusEvent event);

        /** The controller asked for a full resynchronisation (missed events, or the buffer was too old). */
        default void onResync() {
        }

        /** Called after every successful connect. */
        default void onConnected() {
        }
    }

    private static final System.Logger LOG = System.getLogger(EventStream.class.getName());
    private static final String RESYNC = "resync";
    private static final Duration CONNECT_TIMEOUT = Duration.ofSeconds(10);

    private final GrusConfig config;
    private final String types;
    private final Listener listener;
    private final Backoff backoff;
    private final HttpClient http;
    private volatile boolean closed;
    private volatile String lastEventId = "";
    private volatile InputStream body;
    private volatile Thread thread;
    /** Failed attempts since the last successful connect. Only touched by the stream thread. */
    private int failures;

    private EventStream(GrusConfig config, Collection<String> types, Listener listener, Backoff backoff) {
        this.config = config;
        this.types = types.stream()
                .map(type -> URLEncoder.encode(type, StandardCharsets.UTF_8))
                .collect(Collectors.joining(","));
        this.listener = listener;
        this.backoff = backoff;
        this.http = HttpClient.newBuilder().connectTimeout(CONNECT_TIMEOUT).build();
    }

    /**
     * Starts the stream on a new virtual thread.
     *
     * @param types event types to subscribe to, e.g. {@code instance.state} or {@code instance.*}
     */
    public static EventStream start(GrusConfig config, Collection<String> types, Listener listener, Backoff backoff) {
        Objects.requireNonNull(config, "config");
        Objects.requireNonNull(types, "types");
        Objects.requireNonNull(listener, "listener");
        Objects.requireNonNull(backoff, "backoff");
        EventStream stream = new EventStream(config, types, listener, backoff);
        stream.thread = Thread.ofVirtual().name("grus-events").start(stream::run);
        return stream;
    }

    @Override
    public void close() {
        closed = true;
        Thread current = thread;
        if (current != null) {
            current.interrupt();
        }
        closeBody();
    }

    private void run() {
        while (!closed) {
            try {
                connectOnce();
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return;
            } catch (IOException | GrusException e) {
                if (closed) {
                    return;
                }
                LOG.log(System.Logger.Level.WARNING, "Grus event stream failed, reconnecting: {0}", e.toString());
            }
            if (closed) {
                return;
            }
            try {
                backoff.sleep(failures);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return;
            }
            failures++;
        }
    }

    /** One connection: returns when the server ends the stream, throws when it cannot be read. */
    private void connectOnce() throws IOException, InterruptedException {
        HttpRequest.Builder request = HttpRequest.newBuilder(uri())
                .header("Accept", "text/event-stream")
                .header("Cache-Control", "no-cache")
                .header("Authorization", "Bearer " + config.token())
                .GET();
        String resumeFrom = lastEventId;
        if (!resumeFrom.isEmpty()) {
            request.header("Last-Event-ID", resumeFrom);
        }
        HttpResponse<InputStream> response = http.send(request.build(), HttpResponse.BodyHandlers.ofInputStream());
        int status = response.statusCode();
        if (status < 200 || status >= 300) {
            String text;
            try (InputStream in = response.body()) {
                text = new String(in.readAllBytes(), StandardCharsets.UTF_8);
            }
            throw new GrusException("Grus event stream returned HTTP " + status, status, text);
        }

        failures = 0;
        body = response.body();
        if (closed) {
            closeBody();
            return;
        }
        notifyConnected();
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(body, StandardCharsets.UTF_8))) {
            SseParser parser = new SseParser(resumeFrom, this::dispatch);
            String line;
            while (!closed && (line = reader.readLine()) != null) {
                parser.line(line);
            }
        } finally {
            body = null;
        }
    }

    private void dispatch(SseParser.Message message) {
        lastEventId = message.id();
        try {
            if (RESYNC.equals(message.type())) {
                listener.onResync();
                return;
            }
            JsonNode data;
            try {
                data = Json.MAPPER.readTree(message.data());
            } catch (JsonProcessingException e) {
                LOG.log(System.Logger.Level.WARNING, "Skipping event {0} with malformed JSON data", message.type());
                return;
            }
            listener.onEvent(new GrusEvent(message.id(), message.type(), data));
        } catch (RuntimeException e) {
            LOG.log(System.Logger.Level.WARNING, "Grus event listener failed for {0}", message.type());
            LOG.log(System.Logger.Level.DEBUG, "listener failure", e);
        }
    }

    private void notifyConnected() {
        try {
            listener.onConnected();
        } catch (RuntimeException e) {
            LOG.log(System.Logger.Level.WARNING, "Grus onConnected listener failed");
        }
    }

    private void closeBody() {
        InputStream current = body;
        if (current != null) {
            try {
                current.close();
            } catch (IOException ignored) {
                // Closing only unblocks the reader; nothing to recover.
            }
        }
    }

    private URI uri() {
        String base = config.url().toString();
        if (base.endsWith("/")) {
            base = base.substring(0, base.length() - 1);
        }
        return URI.create(base + "/v1/events?types=" + types);
    }
}
