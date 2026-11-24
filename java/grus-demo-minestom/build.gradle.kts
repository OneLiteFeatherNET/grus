import com.github.jengelman.gradle.plugins.shadow.tasks.ShadowJar

plugins {
    application
    alias(libs.plugins.shadow)
}

val mainClassName = "net.onelitefeather.grus.demo.DemoServer"

dependencies {
    implementation(project(":grus-minestom"))
    implementation(libs.minestom)
}

application {
    mainClass.set(mainClassName)
}

// The plain jar must not share the fat jar's file name, or shadow reads its own output as input.
tasks.named<Jar>("jar") {
    archiveClassifier.set("plain")
}

tasks.named<ShadowJar>("shadowJar") {
    archiveClassifier.set("")
    // Jackson is shaded and relocated so it cannot clash with anything else on the server class path.
    relocate("com.fasterxml.jackson", "net.onelitefeather.grus.shaded.jackson")
    mergeServiceFiles()
    manifest {
        attributes["Main-Class"] = mainClassName
    }
}

// Docker build context: the fat jar as app.jar next to the Dockerfile.
//   docker build -t grus-demo-minestom build/docker-context
val dockerContext by tasks.registering(Sync::class) {
    group = "distribution"
    description = "Assembles the Docker build context for the demo lobby image."
    dependsOn(tasks.named("shadowJar"))
    from(tasks.named<ShadowJar>("shadowJar")) {
        rename { "app.jar" }
    }
    from("src/docker") {
        include("Dockerfile")
    }
    into(layout.buildDirectory.dir("docker-context"))
}
