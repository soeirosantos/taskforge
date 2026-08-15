# Sandbox for autonomous experiment runs.
#
# Claude Code runs here with --dangerously-skip-permissions. That is only
# reasonable because of what this container can reach: the repository (mounted)
# and nothing else that matters. No host home directory, no SSH keys, no
# keychain, no other projects.
#
# It also serves the experiment. Toolchains are pinned here rather than
# inherited from the host, and each run gets a fresh HOME, so the task store and
# build caches cannot leak between arms.

FROM debian:bookworm-slim

ARG GO_VERSION=1.23.5
ARG RUST_VERSION=1.83.0
ARG NODE_MAJOR=22

# The agent's own version is apparatus: a different CLI build is a different
# experiment. Left unpinned, `npm install -g` silently changes agent behavior on
# every rebuild — which already cost one run, when an unpinned upgrade changed
# how the task tools are gated.
ARG CLAUDE_CODE_VERSION=2.1.233

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git jq ripgrep build-essential pkg-config \
      libssl-dev python3 python3-venv tini \
    && rm -rf /var/lib/apt/lists/*

# Node (for the Claude Code CLI)
RUN curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# Go
RUN ARCH="$(dpkg --print-architecture)" \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" \
       -o /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz
ENV PATH="/usr/local/go/bin:${PATH}"

# Rust
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:/usr/local/go/bin:$PATH
RUN curl -fsSL https://sh.rustup.rs \
      | sh -s -- -y --no-modify-path --default-toolchain "${RUST_VERSION}" --profile minimal \
    && chmod -R a+w "$CARGO_HOME" "$RUSTUP_HOME"

RUN npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"

# Run as a non-root user whose uid matches the host user, so files the agent
# creates in the mounted repo are owned by you, not root.
ARG UID=1000
ARG GID=1000
RUN groupadd -g "${GID}" agent 2>/dev/null || true \
    && useradd -m -u "${UID}" -g "${GID}" -s /bin/bash agent 2>/dev/null || true \
    && mkdir -p /home/agent && chown -R "${UID}:${GID}" /home/agent

WORKDIR /work
USER ${UID}:${GID}

ENV HOME=/home/agent \
    GOPATH=/home/agent/go \
    GOFLAGS=-mod=mod

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["bash"]
