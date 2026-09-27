# Dockerfile base-image digest pinning

Every Dockerfile in this repo (`Dockerfile.api`, `Dockerfile.worker`, `Dockerfile.agent`) pins both the Go build-stage base and the distroless runtime base to an immutable `sha256` digest. The tag half of the value (`golang:1.23.4-alpine3.21`) is documentation; the digest half is the actual contract.

## Why

Floating tags like `golang:1.23-alpine` or `gcr.io/distroless/static-debian12:nonroot` can be repointed at any time by upstream. A repointed base means:

- Reproducibility breaks. The same source tree produces different binaries depending on when you build.
- Supply-chain surface widens. A compromised upstream tag rolls out silently on the next build.
- Cosign / sigstore signature verification can drift out from under the build.

Pinning to `image:tag@sha256:...` resolves all three. Docker refuses to pull if the registry serves a different digest for that tag.

## When to bump

- **Renovate PR**: the bot opens a PR per base image when a new upstream digest publishes. Default merge cadence is weekly. Always review the upstream changelog before merging.
- **Manual security fix**: when `govulncheck` or `trivy` flags the base, bump immediately. Don't wait for Renovate.
- **Go minor / patch version**: bumping `golang:1.23.4` → `1.23.5` follows the same flow as any other digest bump.

## How to bump (manual)

The three Dockerfiles share the same two `ARG` defaults. Update them together so all binaries built from this repo run on the same digest.

```bash
# Pull the new digest from the registry.
NEW_GO_DIGEST=$(docker manifest inspect --verbose golang:1.23.5-alpine3.21 \
  | jq -r '.Descriptor.digest')
NEW_DISTROLESS_DIGEST=$(docker manifest inspect --verbose \
  gcr.io/distroless/static-debian12:nonroot \
  | jq -r '.Descriptor.digest')

# Update all three Dockerfiles (sed targets the ARG lines).
for f in Dockerfile.api Dockerfile.worker Dockerfile.agent; do
  sed -i "s|^ARG GO_BUILDER_IMAGE=.*|ARG GO_BUILDER_IMAGE=golang:1.23.5-alpine3.21@${NEW_GO_DIGEST}|" "$f"
  sed -i "s|^ARG DISTROLESS_RUNTIME_IMAGE=.*|ARG DISTROLESS_RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@${NEW_DISTROLESS_DIGEST}|" "$f"
done
```

## CI verification

`release.yml` calls `cosign verify` against the distroless base before the build step runs. If the digest no longer matches the published signature (registry hijack, downgrade attack), the release fails closed.

## Local override

To build against an unpinned base in dev (e.g. an arch the pinned digest doesn't ship), pass the ARG at build time:

```bash
docker build \
  --build-arg GO_BUILDER_IMAGE=golang:1.23-alpine \
  --build-arg DISTROLESS_RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot \
  -f Dockerfile.api -t optiqor-api:dev .
```

Never use the override path in CI or release builds.
