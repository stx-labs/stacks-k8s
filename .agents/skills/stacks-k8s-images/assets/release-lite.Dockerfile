# Faster local stacks-core actor image: thin LTO, no debug info, actor binaries only.
# Build with a stacks-core worktree as the context. Timing and codegen differ from
# release images built with the root Dockerfile's fat-LTO `release` profile.
FROM rust:bookworm AS build

ARG STACKS_NODE_VERSION="No Version Info"
ARG GIT_BRANCH="No Branch Info"
ARG GIT_COMMIT="No Commit Info"
ARG CARGO_BUILD_JOBS=8

WORKDIR /src
COPY . .
RUN rustup toolchain install stable
RUN CARGO_PROFILE_RELEASE_LITE_DEBUG=false cargo build --profile release-lite \
      --jobs "$CARGO_BUILD_JOBS" -p stacks-node -p stacks-signer \
      --features stacks-node/monitoring_prom,stacks-node/slog_json,stacks-signer/monitoring_prom \
 && mkdir /out \
 && cp target/release-lite/stacks-node target/release-lite/stacks-signer /out/

FROM debian:bookworm-slim
COPY --from=build /out/stacks-node /out/stacks-signer /bin/
CMD ["stacks-node", "mainnet"]
