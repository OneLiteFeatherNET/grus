package net.onelitefeather.grus.velocity;

import org.junit.jupiter.api.Test;

import java.util.Map;
import java.util.OptionalInt;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

class RegionLookupTest {

    @Test
    void pathNamesThePoolAndTheLookupRoute() {
        assertEquals("/v1/pools/world/cells/at", RegionLookup.path("world"),
                "the lookup must use the cells/at route of the pool");
    }

    @Test
    void pathEncodesUnusualPoolNames() {
        assertEquals("/v1/pools/a%20b%2Fc/cells/at", RegionLookup.path("a b/c"),
                "spaces and slashes in the pool name must be percent-encoded");
    }

    @Test
    void queryCarriesNegativeBlockCoordinatesAsGiven() {
        assertEquals(Map.of("x", "-100", "z", "250"), RegionLookup.query(-100, 250),
                "x and z must be sent as plain integers, the controller converts them to chunks");
    }

    @Test
    void parseReadsCellInstanceAndAddress() {
        RegionLookup.Owner owner = RegionLookup.parse("""
                {"cell":"r1","bounds":{"minX":32,"minZ":0,"maxX":63,"maxZ":31},
                 "instance":"world-ab12c","address":"10.0.0.7:25565"}""");

        assertEquals(new RegionLookup.Owner("r1", "world-ab12c", "10.0.0.7:25565"), owner,
                "the answer must be mapped, unknown fields such as bounds ignored");
    }

    @Test
    void parseRejectsAnAnswerWithoutInstance() {
        assertThrows(IllegalArgumentException.class, () -> RegionLookup.parse("{\"cell\":\"r1\"}"),
                "a cell without an instance cannot be connected to");
    }

    @Test
    void parseRejectsInvalidJson() {
        assertThrows(IllegalArgumentException.class, () -> RegionLookup.parse("<html>"),
                "a body that is not JSON must be rejected");
    }

    @Test
    void parseCoordinateAcceptsIntegersOnly() {
        assertEquals(OptionalInt.of(-42), RegionCommand.parseCoordinate("-42"), "negative integers are valid");
        assertEquals(OptionalInt.empty(), RegionCommand.parseCoordinate("4.5"), "decimals are not block coordinates");
        assertEquals(OptionalInt.empty(), RegionCommand.parseCoordinate("abc"), "words are not coordinates");
    }
}
