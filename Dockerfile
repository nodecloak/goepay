FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY db ./db
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api && CGO_ENABLED=0 go build -trimpath -o /out/cli ./cmd/cli && CGO_ENABLED=0 go build -trimpath -o /out/worker ./cmd/worker && CGO_ENABLED=0 go build -trimpath -o /out/settlement-scheduler ./cmd/settlement-scheduler && CGO_ENABLED=0 go build -trimpath -o /out/settlement-auto-worker ./cmd/settlement-auto-worker && CGO_ENABLED=0 go build -trimpath -o /out/printing-worker ./cmd/printing-worker && CGO_ENABLED=0 go build -trimpath -o /out/referral-scheduler ./cmd/referral-scheduler && CGO_ENABLED=0 go build -trimpath -o /out/merchant-notice-scheduler ./cmd/merchant-notice-scheduler
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
COPY --from=build /out/ /app/
USER app
WORKDIR /app
ENTRYPOINT ["/app/api"]

