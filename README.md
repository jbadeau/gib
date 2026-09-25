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

The tar is both an OCI image layout and a `docker save` archive, the way
Docker's own `save` writes one: every blob once under `blobs/sha256/`,
the image's manifest among them with its exact bytes, `index.json` naming
it and `manifest.json` naming its config and layers. `docker load` reads
it, and `gib push` pushes the very manifest the build wrote.

### Push an image tarball

```sh
gib push my-image.tar my-registry.example.com/my-app:1.4.0
```

Build once, push later: `push` builds nothing. It pushes every blob and
every manifest with the bytes the tarball holds, so the digest the
registry reports, printed on stdout, is the digest the tarball was
written with; only the tag is new.

- **OCI image layout** (`index.json`, as `gib build`, `crane pull
  --format=oci` or apko write): an `index.json` naming one image pushes
  that image; one naming an index pushes the index, children and all.
  Blobs are found by their content wherever the tarball keeps them, so
  apko's tarball, whose `index.json` is itself the index of every
  architecture it built, is pushed as that multi-arch index.
- **docker-save archive** (`manifest.json` only): the config and layers
  are pushed as they are, under the Docker schema 2 manifest describing
  them, the manifest `crane push` or a registry pull of `docker load`'s
  image would carry. An archive of compressed layers pushes those bytes;
  one of uncompressed layers has them compressed first, so its digest is
  that of the compressed image.

The reference names a tag; one naming a digest is refused, the digest
being the tarball's. A tarball holding several tagged images is pushed
only when exactly one of them is tagged in the reference's repository.
Credentials resolve as for `gib build`: `--to-username`/`--to-password`,
then `--username`/`--password`, then `--to-credential-helper` or
`--credential-helper`, then the Docker config (`~/.docker/config.json`).

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

```
gib push <tarball> <reference> [options]
```

| Option | Description |
|---|---|
| `--to-username / --to-password` | Target registry credentials |
| `--to-credential-helper` | Target Docker credential helper suffix |
| `--username / --password`, `--credential-helper` | Fallbacks for the above |
| `--allow-insecure-registries` | Allow HTTP registries |

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
