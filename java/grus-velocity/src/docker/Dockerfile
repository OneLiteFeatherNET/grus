# syntax=docker/dockerfile:1.7
FROM eclipse-temurin:25.0.3_9-jre

# Velocity 4.3.1 (PaperMC, STABLE build 45). The checksum makes the build fail if the download changes.
ADD --chmod=0644 --checksum=sha256:0844bcabdcdb4f5b875906fd4c3365bc1bb76918e42bc595ed88ad195258dd75 \
    https://fill-data.papermc.io/v1/objects/0844bcabdcdb4f5b875906fd4c3365bc1bb76918e42bc595ed88ad195258dd75/velocity-4.3.1-45.jar \
    /opt/velocity/velocity.jar

RUN useradd -u 10001 -r -M velocity \
 && mkdir -p /data /opt/velocity \
 && chown 10001:10001 /data

COPY velocity.toml /opt/velocity/velocity.toml
COPY plugins/ /opt/velocity/plugins/
COPY --chmod=0755 entrypoint.sh /opt/velocity/entrypoint.sh

WORKDIR /data
USER 10001
EXPOSE 25565
ENTRYPOINT ["/opt/velocity/entrypoint.sh"]
