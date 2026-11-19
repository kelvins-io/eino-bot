# syntax=docker/dockerfile:1
# 默认 final stage = from-source（容器内编译）。
# 宿主机交叉编译产物：先 make docker-bin，再
#   docker build --target runtime -t eino-bot:local .

# --- in-container build ---
FROM golang:1.24-bookworm AS build
WORKDIR /src
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ENV GOTOOLCHAIN=auto \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/eino-bot ./cmd/bot

FROM debian:bookworm-slim AS from-source
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 --shell /usr/sbin/nologin appuser
WORKDIR /app
COPY --from=build /out/eino-bot /usr/local/bin/eino-bot
COPY data/docs /app/data/docs
COPY data/resumes /app/data/resumes
RUN mkdir -p /data && chown -R appuser:appuser /app /data
USER appuser
ENV HOST=0.0.0.0 \
    DATA_DIR=/data \
    SQLITE_PATH=/data/bot.db \
    BOT_DOCS_ROOT=/app/data/docs \
    BOT_RESUME_ROOT=/app/data/resumes
EXPOSE 12120 8090
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=8 \
  CMD curl -fsS "http://127.0.0.1:${PORT:-12120}/healthz" || exit 1
ENTRYPOINT ["eino-bot"]

# --- optional: host-built Linux binary ---
FROM debian:bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 --shell /usr/sbin/nologin appuser
WORKDIR /app
COPY bin/eino-bot-linux /usr/local/bin/eino-bot
COPY data/docs /app/data/docs
COPY data/resumes /app/data/resumes
RUN mkdir -p /data && chown -R appuser:appuser /app /data \
    && chmod +x /usr/local/bin/eino-bot
USER appuser
ENV HOST=0.0.0.0 \
    DATA_DIR=/data \
    SQLITE_PATH=/data/bot.db \
    BOT_DOCS_ROOT=/app/data/docs \
    BOT_RESUME_ROOT=/app/data/resumes
EXPOSE 12120 8090
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=8 \
  CMD curl -fsS "http://127.0.0.1:${PORT:-12120}/healthz" || exit 1
ENTRYPOINT ["eino-bot"]
