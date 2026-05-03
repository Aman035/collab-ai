# Multi-stage demo image for collab.
# Stage 1 builds the collab CLI plus the Gensyn Axl node binary.
# Stage 2 is the runtime: node + claude code + the two prebuilt binaries.
# Each container gets its own ~/.claude (mounted volume) so the two
# containers can be logged in to two different Claude accounts.

# ─── Stage 1: builder ────────────────────────────────────────────────
FROM golang:1.25-bookworm AS builder

ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=auto

WORKDIR /src

# Cache deps first.
COPY go.mod go.sum ./
RUN go mod download

# Build collab from the local source tree.
COPY . .
RUN go build -ldflags="-s -w" -o /out/collab ./cmd/collab

# Build the Axl node binary from upstream so the container does not
# clone + go build on first session.
RUN git clone --depth=1 https://github.com/gensyn-ai/axl.git /tmp/axl \
 && cd /tmp/axl \
 && go build -ldflags="-s -w" -o /out/axl-node ./cmd/node


# ─── Stage 2: runtime ────────────────────────────────────────────────
FROM node:22-bookworm-slim AS runtime

# OS deps:
#   git, ca-certificates: claude code + general TLS
#   tini: clean signal handling so docker stop is fast
#   iproute2: `ip addr` so the demo can show distinct container IPs
#   less, procps, vim-tiny: judge-friendly debugging from the shell
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      git curl ca-certificates tini iproute2 less procps vim-tiny \
 && rm -rf /var/lib/apt/lists/*

# Claude Code CLI.
RUN npm install --global --no-audit --no-fund @anthropic-ai/claude-code \
 && npm cache clean --force

# Drop the prebuilt binaries in.
COPY --from=builder /out/collab        /usr/local/bin/collab
COPY --from=builder /out/axl-node      /root/.collab/bin/axl-node
RUN chmod +x /usr/local/bin/collab /root/.collab/bin/axl-node

# Tell collab to use the prebuilt axl-node and skip the bootstrap.
ENV COLLAB_AXL_NODE=/root/.collab/bin/axl-node \
    HOME=/root \
    TERM=xterm-256color

WORKDIR /root

# Tini reaps zombies and forwards signals cleanly.
ENTRYPOINT ["/usr/bin/tini", "--"]
# Keep the container alive so we can `docker exec -it` into it.
CMD ["sleep", "infinity"]
