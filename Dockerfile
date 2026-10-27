FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY db ./db
ARG GOEPAY_VERSION=development
ARG GOEPAY_SOURCE_SHA=
ARG GOEPAY_COMMIT=
RUN set -eu; export CGO_ENABLED=0; for target in api cli worker settlement-scheduler settlement-auto-worker printing-worker referral-scheduler merchant-notice-scheduler task-scheduler risk-check; do go build -trimpath -ldflags="-X goepay/internal/systemupdate.Version=${GOEPAY_VERSION} -X goepay/internal/systemupdate.SourceSHA=${GOEPAY_SOURCE_SHA} -X goepay/internal/systemupdate.Commit=${GOEPAY_COMMIT}" -o "/out/${target}" "./cmd/${target}"; done
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
COPY --from=build /out/ /app/
USER app
WORKDIR /app
ENTRYPOINT ["/app/api"]

