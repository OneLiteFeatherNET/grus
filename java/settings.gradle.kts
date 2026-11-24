rootProject.name = "grus-java"

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        mavenCentral()
        // velocity-api is published by PaperMC, not on Maven Central.
        maven("https://repo.papermc.io/repository/maven-public/")
    }
}

include("grus-sdk")
include("grus-minestom")
include("grus-demo-minestom")
include("grus-velocity")
