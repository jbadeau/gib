<p align="center">
  <img src="https://img.shields.io/badge/go-%2300ADD8.svg?style=for-the-badge&logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/docker-%230db7ed.svg?style=for-the-badge&logo=docker&logoColor=white" alt="Docker" />
  <img src="https://img.shields.io/badge/OCI-%23262261.svg?style=for-the-badge&logo=linux-containers&logoColor=white" alt="OCI" />
</p>

<h1 align="center">Gib</h1>

<p align="center">
  <strong>Build containers. Skip the daemon.</strong>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &nbsp;|&nbsp;
  <a href="#features">Features</a> &nbsp;|&nbsp;
  <a href="#cli-reference">CLI Reference</a> &nbsp;|&nbsp;
  <a href="#go-library">Go Library</a>
</p>

---

## What is Gib?

Gib is a lightweight, daemonless container image builder and a drop-in replacement for [Jib CLI](https://github.com/GoogleContainerTools/jib/tree/master/jib-cli). One static binary, no JVM. Your existing `jib.yaml` files just work.

## Features

- **Drop-in Jib replacement** — 100% compatible with `jib.yaml` build files
- **No runtime dependencies** — builds container images directly, no daemon or JVM needed
- **Go library** — use programmatically in your Go applications
- **Powered by [go-containerregistry](https://github.com/google/go-containerregistry)** — battle-tested container image library
- **Modern CLI** — powered by [Fang](https://github.com/charmbracelet/fang)

## Quick Start

### Install

**With [mise-gib](https://github.com/jbadeau/mise-gib)** (recommended):

```sh
mise plugin install gib https://github.com/jbadeau/mise-gib.git
mise install gib@latest
mise use gib@latest
```

**With `go install`:**

```sh
go install github.com/jbadeau/gib/cmd/gib@latest
```

### Build an image

```sh
gib build --target=my-registry.example.com/my-app:latest
```

### Build to a tar file

```sh
gib build --target=tar://my-image.tar
```

### Minimal `jib.yaml`

```yaml
apiVersion: jib/v1alpha1
kind: BuildFile

from:
  image: ubuntu

entrypoint: ["/app/run.sh"]

layers:
  entries:
    - name: app
      files:
        - src: .
          dest: /app
```

## CLI Reference

```
gib build --target <image> [options]
```

| Option | Description |
|---|---|
| `-t, --target` | **(required)** Target image reference or `tar://<path>` |
| `-b, --build-file` | Build file path (default: `jib.yaml`) |
| `-c, --context` | Build context directory (default: `.`) |
| `-p, --parameter` | Template parameter `key=value` (repeatable) |
| `--from` | Override base image |
| `--image-format` | `Docker` or `OCI` (default: `Docker`) |
| `--additional-tags` | Extra tags for registry targets |
| `--credential-helper` | Docker credential helper suffix |
| `--username / --password` | Registry credentials |

Run `gib build --help` for the full list of options.

## Go Library

Use Gib programmatically to build container images in your Go applications:

```go
builder := gib.From("ubuntu:22.04").
    SetEntrypoint("sh", "run.sh").
    SetUser("appuser").
    SetWorkingDirectory("/app")

result, err := builder.Containerize(
    context.Background(),
    gib.ToRegistry("my-registry.example.com/app:v1"),
)
```

Or build from an existing `jib.yaml`:

```go
spec, _ := buildfile.Parse("jib.yaml", nil)
builder, _ := buildfile.Convert(spec, ".", nil)
result, _ := builder.Containerize(ctx, gib.ToTar("image.tar"))
```

## License

[Apache 2.0](LICENSE)
