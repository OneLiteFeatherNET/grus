package net.onelitefeather.grus.velocity;

import com.velocitypowered.api.command.SimpleCommand;
import com.velocitypowered.api.proxy.Player;
import com.velocitypowered.api.proxy.ProxyServer;
import net.kyori.adventure.text.Component;
import net.onelitefeather.grus.sdk.GrusException;
import org.slf4j.Logger;

import java.util.List;
import java.util.OptionalInt;

/**
 * {@code /region <x> <z>} connects the player to the server that owns the block position in the sharded pool
 * named by {@code GRUS_SHARD_POOL}.
 */
public final class RegionCommand implements SimpleCommand {

    static final String PERMISSION = "grus.region";

    private final ProxyServer proxy;
    private final RegionLookup lookup;
    private final Logger logger;

    public RegionCommand(ProxyServer proxy, RegionLookup lookup, Logger logger) {
        this.proxy = proxy;
        this.lookup = lookup;
        this.logger = logger;
    }

    @Override
    public boolean hasPermission(Invocation invocation) {
        return invocation.source().hasPermission(PERMISSION);
    }

    @Override
    public void execute(Invocation invocation) {
        if (!(invocation.source() instanceof Player player)) {
            invocation.source().sendMessage(Component.text("Only players can use /region."));
            return;
        }
        String[] args = invocation.arguments();
        OptionalInt x = args.length == 2 ? parseCoordinate(args[0]) : OptionalInt.empty();
        OptionalInt z = args.length == 2 ? parseCoordinate(args[1]) : OptionalInt.empty();
        if (x.isEmpty() || z.isEmpty()) {
            player.sendMessage(Component.text("Usage: /region <x> <z> (block coordinates)"));
            return;
        }
        // The controller call blocks, so it must not run on Velocity's command thread.
        Thread.ofVirtual().name("grus-region").start(() -> connect(player, x.getAsInt(), z.getAsInt()));
    }

    private void connect(Player player, int x, int z) {
        try {
            lookup.find(x, z).ifPresentOrElse(
                    owner -> proxy.getServer(owner.instance()).ifPresentOrElse(
                            server -> player.createConnectionRequest(server).fireAndForget(),
                            () -> player.sendMessage(Component.text("The server of that region is not available yet."))),
                    () -> player.sendMessage(Component.text("No server serves that position right now.")));
        } catch (GrusException e) {
            logger.warn("Grus region lookup failed with status {}: {}", e.status(), e.getMessage());
            player.sendMessage(Component.text("The region lookup failed, try again later."));
        }
    }

    @Override
    public List<String> suggest(Invocation invocation) {
        return List.of();
    }

    /** Parses a block coordinate; empty when the text is not an integer. */
    static OptionalInt parseCoordinate(String text) {
        try {
            return OptionalInt.of(Integer.parseInt(text.trim()));
        } catch (NumberFormatException e) {
            return OptionalInt.empty();
        }
    }
}
