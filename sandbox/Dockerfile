FROM node:20-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    curl \
    ca-certificates \
    python3 \
    python3-pip \
    python3-venv \
    jq \
    sudo \
    && rm -rf /var/lib/apt/lists/*

# Passwordless sudo for the node user so agents can install packages on demand
RUN echo "node ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/node && \
    chmod 440 /etc/sudoers.d/node

RUN npm install -g @anthropic-ai/claude-code

# node:slim already ships with a 'node' user (UID 1000)
RUN mkdir -p /workspace /run/claude-host-creds && \
    chmod 755 /run/claude-host-creds && \
    chown node:node /workspace

COPY runner.sh /usr/local/bin/runner
RUN chmod +x /usr/local/bin/runner

USER node
ENV HOME=/home/node

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/runner"]
