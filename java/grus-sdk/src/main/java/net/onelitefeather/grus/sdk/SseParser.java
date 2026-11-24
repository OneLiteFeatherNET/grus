package net.onelitefeather.grus.sdk;

import java.util.function.Consumer;

/**
 * Line-based parser for the Server-Sent Events wire format (WHATWG HTML, section 9.2).
 *
 * <p>Feed it one line at a time without line terminators. A blank line dispatches the buffered
 * event. Comment lines ({@code :...}) are ignored. The last event id persists across events,
 * as the specification requires. Unknown fields such as {@code retry:} are ignored.
 */
final class SseParser {

    /**
     * One dispatched event.
     *
     * @param id   last event id in effect when the event was dispatched, empty if none
     * @param type event name, {@code message} when the stream did not set one
     * @param data data lines joined with {@code \n}
     */
    record Message(String id, String type, String data) {
    }

    private static final String DEFAULT_TYPE = "message";

    private final Consumer<Message> sink;
    private final StringBuilder data = new StringBuilder();
    private boolean hasData;
    private String eventType;
    private String lastEventId;

    SseParser(String lastEventId, Consumer<Message> sink) {
        this.lastEventId = lastEventId == null ? "" : lastEventId;
        this.sink = sink;
    }

    void line(String line) {
        if (line.isEmpty()) {
            dispatch();
            return;
        }
        if (line.charAt(0) == ':') {
            return;
        }
        int colon = line.indexOf(':');
        String field = colon < 0 ? line : line.substring(0, colon);
        String value = colon < 0 ? "" : line.substring(colon + 1);
        if (value.startsWith(" ")) {
            value = value.substring(1);
        }
        switch (field) {
            case "data" -> {
                data.append(value).append('\n');
                hasData = true;
            }
            case "event" -> eventType = value;
            case "id" -> {
                if (!value.contains("\0")) {
                    lastEventId = value;
                }
            }
            default -> {
                // "retry" and unknown fields are ignored.
            }
        }
    }

    String lastEventId() {
        return lastEventId;
    }

    private void dispatch() {
        if (!hasData) {
            eventType = null;
            return;
        }
        // Drop the trailing newline added after the last data line.
        data.setLength(data.length() - 1);
        String type = eventType == null || eventType.isEmpty() ? DEFAULT_TYPE : eventType;
        Message message = new Message(lastEventId, type, data.toString());
        data.setLength(0);
        hasData = false;
        eventType = null;
        sink.accept(message);
    }
}
