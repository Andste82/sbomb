FROM ubuntu:24.04

ARG DEBIAN_FRONTEND=noninteractive

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash-completion \
        binutils \
        binutils-arm-none-eabi \
        build-essential \
        ca-certificates \
        clang \
        cmake \
        curl \
        file \
        g++ \
        gcc \
        gcc-arm-none-eabi \
        gdb-multiarch \
        git \
        git-lfs \
        golang-go \
        jq \
        lcov \
        lld \
        llvm \
        make \
        mingw-w64 \
        ninja-build \
        openssh-client \
        pkg-config \
        python3 \
        python3-pip \
        python3-venv \
        rsync \
        shellcheck \
        unzip \
        valgrind \
        wget \
        xz-utils \
        zip \
    && rm -rf /var/lib/apt/lists/*

RUN update-alternatives --install /usr/bin/cc cc /usr/bin/gcc 100 \
    && update-alternatives --install /usr/bin/c++ c++ /usr/bin/g++ 100 \
    && update-alternatives --install /usr/bin/ld ld /usr/bin/ld.bfd 100

# --- Node.js LTS + Claude Code ------------------------------------------------
RUN curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @anthropic-ai/claude-code

ENV CGO_ENABLED=0

WORKDIR /workspaces/sbomb

CMD ["bash"]