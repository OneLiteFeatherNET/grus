package net.onelitefeather.grus.sdk;

import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

class SseParserTest {

    @Test
    void parsesMultiLineDataIdsAndIgnoresComments() {
        List<SseParser.Message> out = new ArrayList<>();
        SseParser parser = new SseParser("", out::add);

        parser.line(": ping");
        parser.line("id: 7");
        parser.line("event: instance.updated");
        parser.line("data: {\"a\":1}");
        parser.line("data: second");
        parser.line("");

        assertEquals(1, out.size(), "one event must be dispatched");
        SseParser.Message message = out.get(0);
        assertEquals("7", message.id(), "the id field must be carried on the event");
        assertEquals("instance.updated", message.type(), "the event field must be the type");
        assertEquals("{\"a\":1}\nsecond", message.data(), "multi-line data must be joined with newlines");
    }

    @Test
    void lastEventIdPersistsIntoLaterEvents() {
        List<SseParser.Message> out = new ArrayList<>();
        SseParser parser = new SseParser("", out::add);

        parser.line("id: 41");
        parser.line("data: one");
        parser.line("");
        parser.line("data: two");
        parser.line("");

        assertEquals(2, out.size(), "two events must be dispatched");
        assertEquals("41", out.get(1).id(), "an event without its own id must keep the previous id");
        assertEquals("41", parser.lastEventId(), "the parser must expose the last event id");
    }

    @Test
    void eventWithoutTypeIsDispatchedAsMessage() {
        List<SseParser.Message> out = new ArrayList<>();
        SseParser parser = new SseParser("", out::add);

        parser.line("data: plain");
        parser.line("");

        assertEquals("message", out.get(0).type(), "the default event type must be message");
    }

    @Test
    void blankLineWithoutDataDispatchesNothing() {
        List<SseParser.Message> out = new ArrayList<>();
        SseParser parser = new SseParser("", out::add);

        parser.line("event: orphan");
        parser.line("");

        assertTrue(out.isEmpty(), "an event without data must not be dispatched");
    }
}
