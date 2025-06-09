# Pull-Time

A CLI tool to measure and benchmark container image pull times from remote registries using Docker. Just build the binary and we good to use it.

Again thanks for trying out this. Image pulls can take a lot of time in clusters. This is just to understand image pulls. In future I plan to increase its functionality to measure image pull latency in clusters too.

## Features

- **Single Image Pull Measurement**
  - Get time taken to pull image 
  - Usage: `pulltime image [IMAGE_URL]`

- **Benchmark Multiple Registries (Parallel, Detailed, Configurable)**
  - Compare pull times for the same or different images across multiple registries (Docker Hub, GHCR, ECR, Quay, etc.).
  - Supports concurrent pulls (`-c`), per-pull timeout (`-t`), and summary statistics (`-s`).
  - this Outputs a detailed Json
  - Usage: `pulltime benchmark [IMAGE_URLS...] [-c 4] [-t 60] [-s]`

- **CDN or Mirror Validation**
  - Compare pull times between a local mirror (e.g., Harbor) and a remote registry to validate mirror performance.
  - Same as above outputs Json
  - Usage: `pulltime compare [IMAGE_MIRROR] [IMAGE_REMOTE]`

- **CI/CD Optimization**
  - Integrate into your CI pipeline to track and export image pull latency as JSON (to stdout or file).
  - Usage: `pulltime ci [IMAGE_URL] [--output <file>]`

- **Cache/Warmup Analysis**
  - Repeatedly pull and remove an image to measure cold and warm cache pull times.
  - Useful for understanding the impact of Docker cache in CI/CD and developer environments.
  - Usage: `pulltime warmup [IMAGE_URL] -n 5 -d 2000`

## Example Usage

```sh
# Measure pull time for a single image
pulltime image ubuntu:latest

# Benchmark pull times for multiple registries (4 concurrent pulls, 60s timeout, print summary)
pulltime benchmark ubuntu:latest ghcr.io/OWNER/REPO:tag quay.io/ORG/IMAGE:tag -c 4 -t 60 -s

# Compare pull times between a mirror and a remote registry
pulltime compare myharbor.local/library/ubuntu:latest docker.io/library/ubuntu:latest

# Export pull time for CI/CD pipeline
pulltime ci ubuntu:latest --output pull_metrics.json

# Measure cold and warm cache pull times
pulltime warmup ubuntu:latest -n 5 -d 2000
```

## Output
- For `benchmark`, `compare`, `ci`, and `warmup`, results are printed as JSON for easy parsing and reporting. The `ci` command can also write results to a file for pipeline consumption.
- Benchmark output includes: image, registry, pull time (ms), start/end time, bytes downloaded, layer count, and command output.
- Use the `-s` flag with `benchmark` for a summary (min/max/avg/success count).
- Warmup output includes cold and warm cache pull times for each iteration.

## Requirements
- Must have docker and Path must be set