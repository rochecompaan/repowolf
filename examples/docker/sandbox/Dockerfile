# syntax=docker/dockerfile:1

FROM alpine:3 AS fetch
ARG REPOWOLF_VERSION=v0.1.0
ARG REPOWOLF_RELEASE_ROOT=https://github.com/rochecompaan/repowolf/releases/download
ARG TARGETARCH
RUN apk add --no-cache curl ca-certificates
WORKDIR /fetch
RUN set -eu; \
    archive="repowolf_linux_${TARGETARCH}.tar.gz"; \
    base="${REPOWOLF_RELEASE_ROOT}/${REPOWOLF_VERSION}"; \
    curl -fsSLo checksums.txt "${base}/checksums.txt"; \
    curl -fsSLo "$archive" "${base}/${archive}"; \
    awk -v archive="$archive" '$2 == archive { print }' checksums.txt > selected-checksum.txt; \
    test "$(wc -l < selected-checksum.txt)" -eq 1; \
    sha256sum -c selected-checksum.txt; \
    tar -xzf "$archive" repowolf-client

FROM alpine:3
RUN apk add --no-cache git ca-certificates \
    && addgroup -g 65532 agent \
    && adduser -D -u 65532 -G agent agent
COPY --from=fetch /fetch/repowolf-client /usr/local/bin/repowolf-client
RUN ln -s repowolf-client /usr/local/bin/gh \
    && ln -s repowolf-client /usr/local/bin/repowolf-git-ssh
ENV GIT_SSH_COMMAND=repowolf-git-ssh
USER 65532:65532
WORKDIR /home/agent
CMD ["sh"]
