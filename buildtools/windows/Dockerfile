FROM golang:1.25.9-bookworm

ARG WAILS_VERSION=v2.13.0

RUN sed -i \
        -e 's|http://deb.debian.org/debian|https://mirrors.aliyun.com/debian|g' \
        -e 's|http://deb.debian.org/debian-security|https://mirrors.aliyun.com/debian-security|g' \
        /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        binutils-mingw-w64-x86-64 \
        ca-certificates \
        gcc-mingw-w64-x86-64 \
        g++-mingw-w64-x86-64 \
        nsis \
        p7zip-full \
    && rm -rf /var/lib/apt/lists/*

RUN GOBIN=/usr/local/bin go install github.com/wailsapp/wails/v2/cmd/wails@${WAILS_VERSION}

WORKDIR /work

ENTRYPOINT ["/work/buildtools/windows/build.sh"]
