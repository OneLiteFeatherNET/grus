rootProject.name = "grus-java"

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        mavenCentral()
    }
}

include("grus-sdk")
include("grus-minestom")
include("grus-demo-minestom")
// grus-velocity: added together with the Velocity plugin (include("grus-velocity")).
