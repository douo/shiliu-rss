# Build the frontend and CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
# binary locally first. This image packages release artifacts only.
FROM --platform=linux/amd64 gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --chown=65532:65532 --chmod=0555 dist/reader-linux-amd64 /reader
USER 65532:65532
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["/reader"]
CMD ["serve"]
