package net.onelitefeather.grus.demo;

import net.kyori.adventure.text.Component;
import net.minestom.server.Auth;
import net.minestom.server.MinecraftServer;
import net.minestom.server.entity.GameMode;
import net.minestom.server.coordinate.Pos;
import net.minestom.server.command.builder.Command;
import net.minestom.server.event.player.AsyncPlayerConfigurationEvent;
import net.minestom.server.event.player.PlayerSpawnEvent;
import net.minestom.server.instance.InstanceContainer;
import net.minestom.server.instance.block.Block;
import net.onelitefeather.grus.minestom.GrusMinestom;

/**
 * Minimal lobby for the Grus prototype: a flat world, Velocity modern forwarding when
 * {@code VELOCITY_SECRET} is set, a {@code /hub} command, and the Grus integration.
 *
 * <p>Environment: {@code GRUS_PORT} (default 25565), {@code GRUS_INSTANCE} (shown in the welcome
 * message), {@code VELOCITY_SECRET} (optional), plus the {@code GRUS_*} variables read by
 * {@link GrusMinestom}.
 */
public final class DemoServer {

    private static final int SPAWN_Y = 42;

    private DemoServer() {
    }

    public static void main(String[] args) {
        String velocitySecret = System.getenv("VELOCITY_SECRET");
        MinecraftServer server = velocitySecret == null || velocitySecret.isEmpty()
                ? MinecraftServer.init()
                : MinecraftServer.init(new Auth.Velocity(velocitySecret));

        // SIGTERM (docker stop, kubelet) runs the shutdown hook, which saves and disconnects players.
        Runtime.getRuntime().addShutdownHook(new Thread(MinecraftServer::stopCleanly, "grus-demo-shutdown"));

        String instance = System.getenv().getOrDefault("GRUS_INSTANCE", "local");
        InstanceContainer lobby = MinecraftServer.getInstanceManager().createInstanceContainer();
        lobby.setGenerator(unit -> {
            unit.modifier().fillHeight(0, 1, Block.BEDROCK);
            unit.modifier().fillHeight(1, 40, Block.DIRT);
            unit.modifier().fillHeight(40, 41, Block.GRASS_BLOCK);
        });

        MinecraftServer.getGlobalEventHandler()
                .addListener(AsyncPlayerConfigurationEvent.class, event -> {
                    event.setSpawningInstance(lobby);
                    event.getPlayer().setRespawnPoint(new Pos(0.5, SPAWN_Y, 0.5));
                })
                .addListener(PlayerSpawnEvent.class, event -> {
                    if (event.isFirstSpawn()) {
                        event.getPlayer().setGameMode(GameMode.ADVENTURE);
                        event.getPlayer().sendMessage(Component.text("Grus demo lobby " + instance));
                    }
                });

        Command hub = new Command("hub");
        hub.setDefaultExecutor((sender, context) -> sender.sendMessage(Component.text("you are already in a lobby")));
        MinecraftServer.getCommandManager().register(hub);

        int port = parsePort(System.getenv("GRUS_PORT"));
        server.start("0.0.0.0", port);

        GrusMinestom.install().ifPresent(GrusMinestom::ready);
    }

    private static int parsePort(String raw) {
        if (raw == null || raw.isBlank()) {
            return 25565;
        }
        return Integer.parseInt(raw.trim());
    }
}
