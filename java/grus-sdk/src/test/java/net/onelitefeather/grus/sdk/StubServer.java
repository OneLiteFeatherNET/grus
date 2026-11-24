package net.onelitefeather.grus.sdk;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.Objects;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Local HTTP stub on an ephemeral port. Each test creates its own instance and closes it.
 * Handlers run on a cached pool so that a handler may block (for example to hold an SSE stream open).
 */
final class StubServer implements AutoCloseable {

    private final HttpServer server;
    private final ExecutorService executor;

    StubServer() throws IOException {
        server = HttpServer.create(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), 0), 0);
        executor = Executors.newCachedThreadPool();
        server.setExecutor(executor);
        server.start();
    }

    URI url() {
        return URI.create("http://127.0.0.1:" + server.getAddress().getPort());
    }

    GrusConfig config() {
        return GrusConfig.fromMap(java.util.Map.of(
                "GRUS_URL", url().toString(),
                "GRUS_TOKEN", "test-token",
                "GRUS_INSTANCE", "lobby-1",
                "GRUS_POOL", "lobby",
                "GRUS_ROLE", "game"));
    }

    void route(String path, HttpHandler handler) {
        server.createContext(path, handler);
    }

    static void respond(HttpExchange exchange, int status, String body) throws IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().add("Content-Type", "application/json");
        exchange.sendResponseHeaders(status, bytes.length);
        try (OutputStream out = exchange.getResponseBody()) {
            out.write(bytes);
        }
    }

    static String readBody(HttpExchange exchange) throws IOException {
        try (InputStream in = exchange.getRequestBody()) {
            return new String(in.readAllBytes(), StandardCharsets.UTF_8);
        }
    }

    static String header(HttpExchange exchange, String name) {
        return Objects.requireNonNullElse(exchange.getRequestHeaders().getFirst(name), "");
    }

    @Override
    public void close() {
        server.stop(0);
        executor.shutdownNow();
    }
}
