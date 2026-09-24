# ==========================================
# Stage 1: Build Workspace Manager (Go)
# ==========================================
FROM golang:1.27-bookworm AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o workspace-manager ./cmd/workspace-manager

# ==========================================
# Stage 2: Final Multi-Language Workspace
# ==========================================
FROM ubuntu:24.04

LABEL maintainer="Codespace Team"
LABEL description="Full-featured multi-language web IDE and runner environment"

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC
ENV PATH="/root/.cargo/bin:/usr/local/go/bin:/root/.local/bin:${PATH}"

# 1. Install Base System Tools and Libraries
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    wget \
    git \
    gnupg \
    build-essential \
    pkg-config \
    libssl-dev \
    procps \
    htop \
    tree \
    jq \
    unzip \
    zip \
    vim \
    nano \
    sudo \
    tzdata \
    # C/C++ Development
    gcc \
    g++ \
    make \
    cmake \
    gdb \
    # Python Development
    python3 \
    python3-pip \
    python3-venv \
    python3-dev \
    # Java Development
    default-jdk \
    default-jre \
    && rm -rf /var/lib/apt/lists/*

# 2. Install Code-Server
RUN curl -fsSL https://code-server.dev/install.sh | sh

# 3. Install Go
ENV GO_VERSION=1.27.1
RUN wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" && \
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz" && \
    rm "go${GO_VERSION}.linux-amd64.tar.gz"

# 4. Install Node.js and nub toolchain
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y --no-install-recommends nodejs && \
    rm -rf /var/lib/apt/lists/* && \
    corepack enable

# Install nub CLI (Official installer / GitHub release)
RUN curl -fsSL https://nubjs.com/install.sh | bash || \
    npm install -g nubjs || true

# 5. Install Rust via rustup
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable

# 6. Copy Workspace Manager Gateway Binary
COPY --from=builder /build/workspace-manager /usr/local/bin/workspace-manager

# 7. Setup Workspace Directory & Default Settings
WORKDIR /workspace

# Copy default configs if workspace is freshly mounted
COPY workspace.yml /workspace/workspace.yml
COPY sample_app /workspace/sample_app

# Set default Environment Variables
ENV PORT=80
ENV CODE_SERVER_PORT=8080
ENV WORKSPACE_DIR=/workspace
ENV ADMIN_USER=admin
ENV ADMIN_PASS=admin123

# Expose Gateway Port
EXPOSE 80

# Run Workspace Manager as PID 1
ENTRYPOINT ["/usr/local/bin/workspace-manager"]
