FROM docker.io/node:25-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

FROM docker.io/golang:1.26-alpine AS builder
WORKDIR /app
ENV CGO_ENABLED=0
COPY --from=frontend /app/frontend/dist ./static
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN go build -buildvcs=false -o dashboard ./cmd/dashboard

FROM gcr.io/distroless/static-debian11
ARG BUILD_DATE
ARG SOURCE_COMMIT
LABEL org.opencontainers.image.authors="blankdots"
LABEL org.opencontainers.image.created=$BUILD_DATE
LABEL org.opencontainers.image.source="https://github.com/blankdots/cnpg-dashboard"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.title="cnpg-dashboard"
COPY --from=builder /app/dashboard /usr/bin/dashboard
COPY --from=builder /app/static /app/static
ENV STATIC_DIR=/app/static
WORKDIR /app
USER 65534
ENTRYPOINT ["dashboard"]
