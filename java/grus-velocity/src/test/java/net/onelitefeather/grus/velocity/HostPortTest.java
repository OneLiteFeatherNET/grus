package net.onelitefeather.grus.velocity;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

class HostPortTest {

    @Test
    void parseSplitsIpv4HostAndPort() {
        assertEquals(new HostPort("10.0.12.34", 25565), HostPort.parse("10.0.12.34:25565"),
                "an IPv4 address must split at the colon");
    }

    @Test
    void parseKeepsBracketedIpv6Host() {
        assertEquals(new HostPort("2001:db8::1", 25565), HostPort.parse("[2001:db8::1]:25565"),
                "a bracketed IPv6 host must be unwrapped");
    }

    @Test
    void parseRejectsUnbracketedIpv6() {
        assertThrows(IllegalArgumentException.class, () -> HostPort.parse("2001:db8::1:25565"),
                "an IPv6 address without brackets is ambiguous and must be rejected");
    }

    @Test
    void parseRejectsMissingPort() {
        assertThrows(IllegalArgumentException.class, () -> HostPort.parse("10.0.12.34"),
                "an address without a port must be rejected");
    }

    @Test
    void parseRejectsNonNumericPort() {
        assertThrows(IllegalArgumentException.class, () -> HostPort.parse("10.0.12.34:http"),
                "a port that is not a number must be rejected");
    }

    @Test
    void parseRejectsPortOutOfRange() {
        assertThrows(IllegalArgumentException.class, () -> HostPort.parse("10.0.12.34:70000"),
                "a port above 65535 must be rejected");
    }

    @Test
    void parseRejectsNull() {
        assertThrows(IllegalArgumentException.class, () -> HostPort.parse(null),
                "a missing address must be rejected");
    }
}
