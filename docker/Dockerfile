# The Quarters server with Claude Code, for a Linux machine, a NAS or a
# cloud box, used from a browser. See the README (Docker) for the steps:
#   docker compose up -d --build
FROM debian:bookworm-slim

ARG QUARTERS_VERSION=0.8.2
ARG QUARTERS_UID=1000
ARG TARGETARCH

# git for the git tools, ripgrep for search, lsof for the ports list
RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates curl git less lsof openssh-client procps ripgrep tzdata \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://github.com/amithgc/quarters/releases/download/v${QUARTERS_VERSION}/quarters-linux-${TARGETARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin quarters

# Claude Code won't skip its permission prompts as root, so a normal user.
RUN useradd -m -u "$QUARTERS_UID" -s /bin/bash dev
USER dev
WORKDIR /home/dev
RUN curl -fsSL https://claude.ai/install.sh | bash && mkdir -p code .config/quarters
ENV PATH=/home/dev/.local/bin:$PATH SHELL=/bin/bash

COPY entrypoint.sh /usr/local/bin/quarters-entrypoint
EXPOSE 8421
ENTRYPOINT ["quarters-entrypoint"]
CMD ["quarters", "--serve"]
