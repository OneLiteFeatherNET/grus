package net.onelitefeather.grus.velocity;

import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.util.concurrent.CompletionException;

import static org.junit.jupiter.api.Assertions.assertEquals;

class BackendPingTest {

    @Test
    void successLineNamesServerAddressVersionAndCounts() {
        assertEquals("pinged lobby-1 at 10.0.12.34:25565: 1.21.4 3/20",
                BackendPing.pinged("lobby-1", "10.0.12.34:25565", "1.21.4", 3, 20));
    }

    @Test
    void failureLineCarriesTheReason() {
        assertEquals("ping lobby-1 failed: Connection refused",
                BackendPing.failed("lobby-1", new IOException("Connection refused")));
    }

    @Test
    void failureLineUnwrapsCompletionException() {
        assertEquals("ping lobby-1 failed: Connection refused",
                BackendPing.failed("lobby-1", new CompletionException(new IOException("Connection refused"))));
    }

    @Test
    void failureWithoutMessageUsesExceptionClassName() {
        assertEquals("ping lobby-1 failed: java.io.IOException",
                BackendPing.failed("lobby-1", new IOException()));
    }
}
