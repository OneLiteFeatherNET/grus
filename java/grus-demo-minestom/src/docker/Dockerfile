FROM eclipse-temurin:25.0.5.1_2-jre

RUN useradd -u 10001 -r -m grus \
 && mkdir -p /data /opt/grus \
 && chown 10001:10001 /data

COPY app.jar /opt/grus/app.jar

WORKDIR /data
USER 10001
EXPOSE 25565
ENTRYPOINT ["java","-XX:MaxRAMPercentage=75","-jar","/opt/grus/app.jar"]
