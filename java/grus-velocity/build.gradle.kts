import com.github.jengelman.gradle.plugins.shadow.tasks.ShadowJar

plugins {
    alias(libs.plugins.shadow)
}

dependencies {
    implementation(project(":grus-sdk"))
    implementation(platform(libs.jackson.bom))
    implementation(libs.jackson.databind)
    // Velocity provides its API and slf4j at runtime, so neither is shaded.
    compileOnly(libs.velocity.api)
    // The annotation processor generates velocity-plugin.json from @Plugin.
    annotationProcessor(libs.velocity.api)

    testImplementation(libs.velocity.api)
    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testRuntimeOnly(libs.junit.platform.launcher)
}

// The plain jar must not share the fat jar's file name, or shadow reads its own output as input.
tasks.named<Jar>("jar") {
    archiveClassifier.set("plain")
}

tasks.named<ShadowJar>("shadowJar") {
    archiveClassifier.set("")
    // Jackson is shaded and relocated so it cannot clash with other plugins on the proxy.
    relocate("com.fasterxml.jackson", "net.onelitefeather.grus.shaded.jackson")
    mergeServiceFiles()
}
