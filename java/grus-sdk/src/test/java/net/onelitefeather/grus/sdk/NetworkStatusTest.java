package net.onelitefeather.grus.sdk;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

class NetworkStatusTest {

    @Test
    void parsesNetworkPlayersEventPayload() throws Exception {
        GrusEvent event = event("{\"players\":42,\"maxPlayers\":1000,\"proxies\":3}");

        NetworkStatus status = NetworkStatus.from(event);

        assertEquals(new NetworkStatus(42, 1000, 3), status, "players, maxPlayers and proxies must be parsed");
    }

    @Test
    void ignoresFieldsThatAreNotKnownYet() throws Exception {
        GrusEvent event = event("{\"players\":1,\"maxPlayers\":2,\"proxies\":1,\"generation\":9}");

        assertEquals(new NetworkStatus(1, 2, 1), NetworkStatus.from(event),
                "a newer controller may add fields; the SDK must still read the known ones");
    }

    @Test
    void payloadThatIsNotAnObjectIsRejected() throws Exception {
        GrusEvent event = event("\"not a network status\"");

        assertThrows(IllegalArgumentException.class, () -> NetworkStatus.from(event),
                "a malformed network.players payload must be reported, not parsed into zeros");
    }

    @Test
    void eventTypeConstantMatchesControllerContract() {
        assertEquals("network.players", NetworkStatus.EVENT_TYPE, "the controller publishes this SSE event name");
    }

    private static GrusEvent event(String json) throws Exception {
        return new GrusEvent("1", NetworkStatus.EVENT_TYPE, Json.MAPPER.readTree(json));
    }
}
