package net.onelitefeather.grus.sdk;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.core.JsonProcessingException;

/**
 * Payload of an {@code instance.state} event.
 *
 * @param instance instance id
 * @param pool     pool name
 * @param role     role name
 * @param from     previous state
 * @param to       new state
 * @param address  {@code host:port} of the instance, may be null
 * @param reason   controller-provided reason, may be null
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public record InstanceStateChange(
        String instance,
        String pool,
        String role,
        String from,
        String to,
        String address,
        String reason) {

    /**
     * Parses the data of an {@code instance.state} event.
     *
     * @throws IllegalArgumentException when the payload does not match this shape
     */
    public static InstanceStateChange from(GrusEvent event) {
        try {
            return Json.MAPPER.treeToValue(event.data(), InstanceStateChange.class);
        } catch (JsonProcessingException | IllegalArgumentException e) {
            throw new IllegalArgumentException("not an instance.state payload: " + event.type(), e);
        }
    }
}
