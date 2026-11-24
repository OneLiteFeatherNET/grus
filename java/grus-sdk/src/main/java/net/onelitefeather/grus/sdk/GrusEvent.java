package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.databind.JsonNode;

/**
 * A server-sent event from {@code GET /v1/events}.
 *
 * @param id   the SSE {@code id:} (monotonic sequence number as text)
 * @param type the SSE {@code event:} name, e.g. {@code instance.state}
 * @param data the parsed JSON payload of the {@code data:} lines
 */
public record GrusEvent(String id, String type, JsonNode data) {
}
