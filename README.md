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
gib build --target=tar://my-image.tar --name=my-app:latest
```

As Jib writes it, a Docker-format image's tarball is a `docker save`
archive naming the image by every tag, and an OCI-format image's is an
OCI image layout, the manifest among its blobs with its exact bytes.
`docker load` reads either, and `gib push` pushes the very manifest the
build wrote.

### Load into the Docker daemon

```sh
gib build --target=docker://my-app:latest
```

The image tarball is piped into `docker load`, as Jib does.

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
Credentials resolve as for `gib build`.

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
gib build --target <image> [options] [@<argfile>...]
```

`gib build` takes exactly the options of `jib build`, and refuses what
it refuses: a command line Jib rejects exits 2, a build that fails exits
1. The container settings of `jib jar` and `jib war` (`--from`,
`--entrypoint` and the like) belong in the build file.

| Option | Description |
|---|---|
| `-t, --target` | **(required)** Target image: a reference, `registry://<ref>`, `docker://<ref>` or `tar://<path>` |
| `--name` | The image's name in a tarball (required with `tar://`) |
| `-b, --build-file` | Build file path (default: `<context>/jib.yaml`) |
| `-c, --context` | Build context directory (default: `.`) |
| `-p, --parameter` | Template parameter `name=value` (repeatable) |
| `--additional-tags` | Extra tags, comma separated (repeatable) |
| `--credential-helper` | A credential helper: its path, or the suffix of `docker-credential-<suffix>` |
| `--username / --password` | Credentials for both registries; `--password` alone prompts for it |
| `--to-*`, `--from-*` | The same, for the target or the base image registry alone |
| `--allow-insecure-registries` | Reach a registry without TLS verification, then over HTTP |
| `--send-credentials-over-http` | Send credentials over plain HTTP |
| `--image-metadata-out` | Write the image's digest, ID and tags to a JSON file |
| `--verbosity` | `quiet`, `error`, `warn`, `lifecycle` (default), `info` or `debug` |
| `--console` | `auto` (default), `rich` or `plain` |

Without credentials given, they are found where Jib finds them: Podman's
`auth.json`, Docker's `config.json` (its credential helpers too),
`docker-credential-gcr` for `gcr.io` and `docker-credential-ecr-login`
for `amazonaws.com`, and Google's Application Default Credentials.
Registry mirrors are read from Jib's global `config.json`.

Run `gib build --help` for the full list of options.

```
gib push <tarball> <reference> [options]
```

| Option | Description |
|---|---|
| `--to-username / --to-password` | Target registry credentials |
| `--to-credential-helper` | Target registry credential helper |
| `--username / --password`, `--credential-helper` | Fallbacks for the above |
| `--allow-insecure-registries` | Reach a registry without TLS verification, then over HTTP |

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
result, _ := builder.Containerize(ctx, gib.ToTar("image.tar", gib.WithTarImageName("app")))
```

## License

[Apache 2.0](LICENSE)
