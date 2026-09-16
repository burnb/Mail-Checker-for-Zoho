FROM --platform=${BUILDPLATFORM} golang:1.24-alpine AS builder

RUN apk -U upgrade && \
    apk add --update ca-certificates tzdata curl gzip openssh-client git

WORKDIR /build

COPY ./server .

RUN	go mod download

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o ./bin/app ./cmd/main.go

FROM scratch AS runner

# Import certs and timezone data.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

ENV APP_ROOT=/opt/app
ENV PATH=/opt/app

COPY --from=builder /build/bin $APP_ROOT

EXPOSE 8000

USER 1000

WORKDIR $APP_ROOT

ENTRYPOINT ["/opt/app/app"]
