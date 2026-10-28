FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/curu ./cmd/curu
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/curu-migrate ./cmd/curu-migrate
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/curu-initialize ./cmd/curu-initialize

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/curu /curu
COPY --from=build /out/curu-migrate /curu-migrate
COPY --from=build /out/curu-initialize /curu-initialize
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/curu"]
