FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/telegram-relay .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/telegram-relay /telegram-relay
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/telegram-relay"]
