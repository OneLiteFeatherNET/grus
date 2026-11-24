package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import java.net.URI;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;

class GrusConfigTest {

    @Test
    void fromMapReadsAllKeys() {
        GrusConfig config = GrusConfig.fromMap(Map.of(
                "GRUS_URL", "http://grus:8080",
                "GRUS_TOKEN", "secret",
                "GRUS_INSTANCE", "lobby-1",
                "GRUS_POOL", "lobby",
                "GRUS_ROLE", "game"));

        assertEquals(URI.create("http://grus:8080"), config.url(), "the URL must be read");
        assertEquals("secret", config.token(), "the token must be read");
        assertEquals("lobby-1", config.instance(), "the instance must be read");
        assertEquals("lobby", config.pool(), "the pool must be read");
        assertEquals("game", config.role(), "the role must be read");
    }

    @Test
    void fromMapRequiresUrlAndToken() {
        assertThrows(IllegalArgumentException.class,
                () -> GrusConfig.fromMap(Map.of("GRUS_TOKEN", "secret")),
                "a missing GRUS_URL must be rejected");
        assertThrows(IllegalArgumentException.class,
                () -> GrusConfig.fromMap(Map.of("GRUS_URL", "http://grus:8080", "GRUS_TOKEN", " ")),
                "a blank GRUS_TOKEN must be rejected");
    }

    @Test
    void optionalFieldsBecomeNullWhenBlank() {
        GrusConfig config = GrusConfig.fromMap(Map.of(
                "GRUS_URL", "http://grus:8080",
                "GRUS_TOKEN", "secret",
                "GRUS_POOL", ""));

        assertNull(config.pool(), "a blank pool must be treated as absent");
        assertNull(config.instance(), "a missing instance must be null");
    }

    @Test
    void toStringNeverShowsTheToken() {
        GrusConfig config = GrusConfig.fromMap(Map.of("GRUS_URL", "http://grus:8080", "GRUS_TOKEN", "topsecret"));

        assertFalse(config.toString().contains("topsecret"), "the token must not appear in toString");
    }

    @Test
    void instanceStateChangeParsesTheEventPayload() throws Exception {
        var data = new ObjectMapper().readTree(
                "{\"instance\":\"lobby-7f3k\",\"pool\":\"lobby\",\"role\":\"game\",\"from\":\"Starting\","
                        + "\"to\":\"Ready\",\"address\":\"10.0.12.34:25565\"}");

        InstanceStateChange change = InstanceStateChange.from(new GrusEvent("1042", "instance.state", data));

        assertEquals("Starting", change.from(), "from must be parsed");
        assertEquals("Ready", change.to(), "to must be parsed");
        assertNull(change.reason(), "a missing reason must be null");
    }
}
