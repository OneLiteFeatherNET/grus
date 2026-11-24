package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

class GrusClientTest {

    private static final String INSTANCE_JSON = """
            {"id":"lobby-7f3k","pool":"lobby","role":"game","state":"Ready","address":"10.0.12.34:25565",
             "players":3,"maxPlayers":20,"labels":{"map":"spawn"},"host":"k8s-grus-dev"}""";

    @Test
    void instancesSendsBearerTokenAndParsesTopLevelArray() throws IOException {
        try (StubServer server = new StubServer()) {
            AtomicReference<String> authorization = new AtomicReference<>();
            AtomicReference<String> query = new AtomicReference<>();
            server.route("/v1/instances", exchange -> {
                authorization.set(StubServer.header(exchange, "Authorization"));
                query.set(exchange.getRequestURI().getRawQuery());
                StubServer.respond(exchange, 200, "[" + INSTANCE_JSON + "]");
            });
            GrusClient client = new GrusClient(server.config());

            List<Instance> instances = client.instances("lobby", "Ready", null);

            assertEquals("Bearer test-token", authorization.get(), "bearer token must be sent");
            assertEquals("pool=lobby&state=Ready", query.get(), "null filters must be omitted");
            assertEquals(1, instances.size(), "one instance expected");
            Instance lobby = instances.get(0);
            assertEquals("lobby-7f3k", lobby.id(), "id must be parsed");
            assertEquals("Ready", lobby.state(), "state must be parsed");
            assertEquals(3, lobby.players(), "players must be parsed");
            assertEquals(Map.of("map", "spawn"), lobby.labels(), "labels must be parsed");
        }
    }

    @Test
    void statusSendsPlayersMaxPlayersAndLabelsAsJson() throws IOException {
        try (StubServer server = new StubServer()) {
            AtomicReference<String> method = new AtomicReference<>();
            AtomicReference<String> body = new AtomicReference<>();
            server.route("/v1/self/status", exchange -> {
                method.set(exchange.getRequestMethod());
                body.set(StubServer.readBody(exchange));
                StubServer.respond(exchange, 200, INSTANCE_JSON);
            });
            GrusClient client = new GrusClient(server.config());

            Instance result = client.status(3, 20, Map.of("map", "spawn"));

            assertEquals("PUT", method.get(), "status is reported with PUT");
            JsonNode sent = new ObjectMapper().readTree(body.get());
            assertEquals(3, sent.get("players").asInt(), "players must be in the body");
            assertEquals(20, sent.get("maxPlayers").asInt(), "maxPlayers must be in the body");
            assertEquals("spawn", sent.get("labels").get("map").asText(), "labels must be in the body");
            assertEquals("lobby-7f3k", result.id(), "the response instance must be returned");
        }
    }

    @Test
    void nonSuccessResponseThrowsGrusExceptionWithStatusAndBody() throws IOException {
        try (StubServer server = new StubServer()) {
            server.route("/v1/instances/missing", exchange -> StubServer.respond(exchange, 404, "no such instance"));
            GrusClient client = new GrusClient(server.config());

            GrusException error = assertThrows(GrusException.class, () -> client.instance("missing"),
                    "a 404 must be reported as GrusException");

            assertEquals(404, error.status(), "the HTTP status must be kept");
            assertTrue(error.body().contains("no such instance"), "the response body must be kept");
        }
    }
}
