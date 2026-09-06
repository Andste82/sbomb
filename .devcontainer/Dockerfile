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
        libusb-1.0-0 \
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

# --- GitHub CLI ---------------------------------------------------------------
RUN mkdir -p -m 755 /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# --- Node.js LTS + Claude Code ------------------------------------------------
RUN curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g @anthropic-ai/claude-code

# --- Package managers -------------------------------------------------------
# Every version is pinned. The fixture corpus is generated from these tools,
# so an unpinned one would make the corpus unreproducible -- which is the
# property sbomb exists to check in other people's builds.
ARG CONAN_VERSION=2.32.0
ARG WEST_VERSION=1.5.0
ARG VCPKG_COMMIT=04a9d8e5212d01ee1dd9478eadd9caade4f8b0d4
ARG CPM_VERSION=0.43.1
ARG CPM_SHA256=1c40fc102ce9625d7de7eb14f541cab30cc3138dca627f0b0ec40293ce6c2934

# Conan and west live in their own virtual environment: Ubuntu 24.04 marks the
# system interpreter externally managed, and breaking that open would let a
# later apt upgrade fight with pip over the same files.
RUN python3 -m venv /opt/pkgtools \
    && /opt/pkgtools/bin/pip install --no-cache-dir --no-input \
        "conan==${CONAN_VERSION}" "west==${WEST_VERSION}" \
    && ln -s /opt/pkgtools/bin/conan /usr/local/bin/conan \
    && ln -s /opt/pkgtools/bin/west /usr/local/bin/west

RUN git clone --filter=blob:none https://github.com/microsoft/vcpkg /opt/vcpkg \
    && git -C /opt/vcpkg checkout --detach "${VCPKG_COMMIT}" \
    && VCPKG_DISABLE_METRICS=1 /opt/vcpkg/bootstrap-vcpkg.sh -disableMetrics \
    && ln -s /opt/vcpkg/vcpkg /usr/local/bin/vcpkg \
    && rm -rf /opt/vcpkg/.git

RUN mkdir -p /opt/cpm \
    && curl -fsSL -o /opt/cpm/CPM.cmake \
        "https://github.com/cpm-cmake/CPM.cmake/releases/download/v${CPM_VERSION}/CPM.cmake" \
    && echo "${CPM_SHA256}  /opt/cpm/CPM.cmake" | sha256sum -c -

# VCPKG_FORCE_SYSTEM_BINARIES stops vcpkg downloading its own cmake and ninja:
# the fixtures must be built with the versions this image pins, not with
# whatever vcpkg fetches on the day.
ENV VCPKG_ROOT=/opt/vcpkg \
    VCPKG_FORCE_SYSTEM_BINARIES=1 \
    VCPKG_DISABLE_METRICS=1 \
    CPM_PATH=/opt/cpm/CPM.cmake

# --- ESP-IDF (optional) -----------------------------------------------------
# Roughly 2.1 GB: 659 MB of sources and 1.4 GB of toolchains, against 131 MB
# for everything above. Off by default; build with --build-arg WITH_ESP_IDF=1
# when you need to produce or refresh the ESP-IDF fixture, which milestone 20
# captures once and commits.
ARG WITH_ESP_IDF=0
ARG ESP_IDF_VERSION=v5.5
ENV IDF_PATH=/opt/esp-idf \
    IDF_TOOLS_PATH=/opt/esp-idf-tools
RUN if [ "${WITH_ESP_IDF}" = "1" ]; then \
        git clone --depth 1 -b "${ESP_IDF_VERSION}" --recurse-submodules --shallow-submodules \
            https://github.com/espressif/esp-idf "${IDF_PATH}" \
        && "${IDF_PATH}/install.sh" esp32 \
        && rm -rf "${IDF_TOOLS_PATH}/dist"; \
    fi

ENV CGO_ENABLED=0

WORKDIR /workspaces/sbomb

CMD ["bash"]