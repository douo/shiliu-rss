# Build the frontend and CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
# binary locally first. This image packages release artifacts only.
FROM --platform=linux/amd64 gcr.io/distroless/static-debian13:nonroot@sha256:2293b36c7c9082bf4115aab724b4d2cddec82c8eba39bf27ac0517e159acf150
COPY --chown=65532:65532 dist/reader-linux-amd64 /reader
USER 65532:65532
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["/reader"]
CMD ["serve"]
