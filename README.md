# GIB - Go Container Builder

`gib` is a command-line utility for building Docker or [OCI](https://github.com/opencontainers/image-spec) container images from file system content. GIB builds containers [fast and reproducibly without Docker](https://github.com/GoogleContainerTools/jib#goals), powered by [go-containerregistry](https://github.com/google/go-containerregistry).

```sh
# docker not required
$ docker
-bash: docker: command not found
# build and upload an image
$ gib build --target=my-registry.example.com/built-by-gib
```

GIB is a Go port of the [Jib CLI](https://github.com/GoogleContainerTools/jib/tree/master/jib-cli), fully compatible with the `jib.yaml` build file format.

## Table of Contents

* [Get GIB](#get-gib)
  * [Install with `go install`](#install-with-go-install)
  * [Build from Source](#build-from-source)
* [Build Command](#build-command)
  * [Quickstart](#quickstart)
  * [Options](#options)
* [Common Options](#common-options)
  * [Auth/Security](#authsecurity)
  * [Registry Credentials](#registry-credentials)
  * [Info Params](#info-params)
* [References](#references)
  * [Fully Annotated Build File (`jib.yaml`)](#fully-annotated-build-file-jibyaml)
* [Go Library](#go-library)
* [Project Structure](#project-structure)

## Get GIB

### Install with `go install`

```sh
go install github.com/jbadeau/gib/cmd/gib@latest
```

### Build from Source

```sh
git clone https://github.com/jbadeau/gib.git
cd gib
go build -o gib ./cmd/gib
```

## Build Command

This command follows the pattern:

```
gib build --target <image name> [options]
```

### Quickstart

1. Create a hello world script (`script.sh`) containing:
    ```sh
    #!/bin/sh
    echo "Hello World"
    ```

2. Create a build file. The default is a file named `jib.yaml` in the project root.
    ```yaml
    apiVersion: jib/v1alpha1
    kind: BuildFile

    from:
      image: ubuntu

    entrypoint: ["/script.sh"]

    layers:
      entries:
        - name: scripts
          files:
            - properties:
                filePermissions: "755"
              src: script.sh
              dest: /script.sh
    ```

3. Build to a tar file:
    ```sh
    $ gib build --target=tar://jib-cli-quickstart.tar
    ```

4. Or build and push to a registry:
    ```sh
    $ gib build --target=my-registry.example.com/quickstart:latest
    ```

### Options

| Option | Description |
|--------|-------------|
| `-t, --target` | **(required)** Target image reference or `tar://<path>` for tar output |
| `-b, --build-file` | Path to the build file (default: `jib.yaml`) |
| `-c, --context` | Context root directory of the build (default: `.`) |
| `-p, --parameter` | Template parameter to inject into build file, `key=value` (repeatable) |
| `--name` | Image reference for tar targets |
| `--additional-tags` | Additional tags for registry targets (comma-separated) |
| `--from` | Base image override |
| `--image-format` | Image format: `Docker` or `OCI` (default: `Docker`) |
| `--creation-time` | Container creation time in millis since epoch or ISO 8601 |
| `--entrypoint` | Override container entrypoint |
| `--program-args` | Override container CMD |
| `--expose` | Override exposed ports (e.g. `8080`, `8080/udp`) |
| `--volumes` | Override volume mount points |
| `--environment-variables` | Environment variables (`key=value`, repeatable) |
| `--labels` | Container labels (`key=value`, repeatable) |
| `-u, --user` | User to run the container as |
| `--image-metadata-out` | Write result JSON (digest, image ID) to file |

## Common Options

### Auth/Security

```
--allow-insecure-registries    Allow connections to HTTP (non-TLS) registries
--send-credentials-over-http   Allow sending credentials over HTTP (very insecure)
```

### Registry Credentials

Credentials can be specified using credential helpers or username/password. The following options are available:

```
--credential-helper <suffix>        Credential helper for both target and base image registries.
                                    Suffix for an executable named docker-credential-<suffix>
--to-credential-helper <suffix>     Credential helper for the target registry
--from-credential-helper <suffix>   Credential helper for the base image registry

--username <username>               Username for both target and base image registries
--password <password>               Password for both target and base image registries
--to-username <username>            Username for the target registry
--to-password <password>            Password for the target registry
--from-username <username>          Username for the base image registry
--from-password <password>          Password for the base image registry
```

**Note** — Combinations of `credential-helper`, `username`, and `password` flags come with restrictions and can only be used in the following ways:

Only Credential Helper:
1. `--credential-helper`
2. `--to-credential-helper`
3. `--from-credential-helper`
4. `--to-credential-helper`, `--from-credential-helper`

Only Username and Password:
1. `--username`, `--password`
2. `--to-username`, `--to-password`
3. `--from-username`, `--from-password`
4. `--to-username`, `--to-password`, `--from-username`, `--from-password`

Mixed Mode:
1. `--to-credential-helper`, `--from-username`, `--from-password`
2. `--from-credential-helper`, `--to-username`, `--to-password`

### Info Params

```
--help                  Print usage and exit
--verbosity <level>     Set logging verbosity (default: lifecycle)
-v, --version           Print version information
```

## References

### Fully Annotated Build File (`jib.yaml`)

```yaml
# required apiVersion and kind, for compatibility over versions of the cli
apiVersion: jib/v1alpha1
kind: BuildFile

# full base image specification with support for multi-architecture builds
from:
  image: "ubuntu"
  # set platforms for multi architecture builds, defaults to linux/amd64
  platforms:
    - architecture: "arm"
      os: "linux"
    - architecture: "amd64"
      os: "darwin"

# creation time of the container
# can be: millis since epoch (ex: 1000) or an ISO 8601 time (ex: 2020-06-08T14:54:36+00:00)
creationTime: 2000

format: Docker # Docker or OCI

# container environment variables
environment:
  "KEY1": "v1"
  "KEY2": "v2"

# container labels
labels:
  "label1": "l1"
  "label2": "l2"

# volume mount points
volumes:
  - "/volume1"
  - "/volume2"

# exposed ports metadata (port-number/protocol)
exposedPorts:
  - "123/udp"
  - "456"      # default protocol is tcp
  - "789/tcp"

# the user to run the container (does not affect file permissions)
user: "customUser"

workingDirectory: "/home"

entrypoint:
  - "sh"
  - "script.sh"
cmd:
  - "--param"
  - "param"

# file layers of the container
layers:
  properties:                        # file properties applied to all layers
    filePermissions: "123"           # octal file permissions, default is 644
    directoryPermissions: "123"      # octal directory permissions, default is 755
    user: "2"                        # default user is 0
    group: "4"                       # default group is 0
    timestamp: "1232"                # millis since epoch or ISO 8601, default is "epoch + 1 second"
  entries:
    - name: "scripts"                # first layer
      properties:                    # file properties applied to only this layer
        filePermissions: "123"
        # see above for full list of properties...
      files:                         # a list of copy directives constitute a single layer
        - src: "project/run.sh"      # a simple copy directive (inherits layer level file properties)
          dest: "/home/run.sh"       # all 'dest' specifications must be absolute paths on the container
        - src: "scripts"             # a second copy directive in the same layer
          dest: "/home/scripts"
          excludes:                  # exclude all files matching these patterns
            - "**/exclude.me"
            - "**/*.ignore"
          includes:                  # include only files matching these patterns
            - "**/include.me"
          properties:                # file properties applied to only this copy directive
            filePermissions: "123"
            # see above for full list of properties...
    - name: "images"                 # second layer, inherits file properties from global
      files:
        - src: "images"
          dest: "/images"
```

### Layers Behavior

- Copy directives are bound by the following rules for `src`:
  - If `src` is a directory, `dest` is always considered a directory. Directory and contents will be copied over and renamed to `dest`.
  - If `src` is a file:
    - If `dest` ends with `/` then it is considered a target directory — the file will be copied into the directory.
    - If `dest` doesn't end with `/` then it is the target file location — `src` will be copied and renamed to `dest`.

- Permissions for a file or directory that appear in multiple layers will prioritize the *last* layer the file appears in.

- Parent directories that are not explicitly defined in a layer will receive default properties (permissions: `755`, modification-time: epoch+1).

- To exclude a directory *and* all its files:
    ```yaml
    excludes:
      - "**/exclude-dir"
      - "**/exclude-dir/**"
    ```

### Base Image Parameter Inheritance

Parameters that **append** to base image values:
- `volumes`
- `exposedPorts`

Parameters that **append new keys and overwrite existing keys**:
- `labels`
- `environment`

Parameters that are **overwritten**:
- `user`
- `workingDirectory`
- `entrypoint`
- `cmd`

## Go Library

GIB can also be used as a Go library for building container images programmatically:

```go
package main

import (
    "context"
    "fmt"

    "github.com/jbadeau/gib"
)

func main() {
    builder := gib.From("ubuntu:22.04").
        SetEntrypoint("sh", "run.sh").
        SetUser("appuser").
        SetWorkingDirectory("/app").
        SetEnvironment(map[string]string{"ENV": "production"}).
        AddExposedPort(gib.Port{Number: 8080, Protocol: "tcp"})

    result, err := builder.Containerize(
        context.Background(),
        gib.ToTar("image.tar"),
    )
    if err != nil {
        panic(err)
    }

    fmt.Printf("Built: %s (digest: %s)\n", result.TargetImage, result.Digest)
}
```

Or build from a `jib.yaml` file:

```go
package main

import (
    "context"
    "fmt"

    "github.com/jbadeau/gib"
    "github.com/jbadeau/gib/buildfile"
)

func main() {
    spec, _ := buildfile.Parse("jib.yaml", nil)
    builder, _ := buildfile.Convert(spec, ".", nil)

    result, _ := builder.Containerize(
        context.Background(),
        gib.ToRegistry("my-registry.example.com/app:v1"),
    )

    fmt.Printf("Pushed: %s\n", result.Digest)
}
```

## Project Structure

GIB is organized as a Go workspace with two modules:

```
gib/
├── go.work              # Go workspace
├── gib/                 # Core library (module: github.com/jbadeau/gib)
│   ├── gib.go           #   Entry points: From(), FromScratch(), FromImage()
│   ├── builder.go       #   Fluent ContainerBuilder API
│   ├── containerizer.go #   Registry push and tar output
│   ├── credential.go    #   Docker credential helper support
│   ├── image_source.go  #   Base image sources (registry, tar, scratch)
│   ├── buildfile/       #   jib.yaml parsing, validation, and conversion
│   ├── internal/        #   Build pipeline and reproducible layer creation
│   └── testdata/        #   Test fixtures
└── cmd/gib/             # CLI (module: github.com/jbadeau/gib/cmd/gib)
    ├── main.go          #   Fang/Cobra entry point
    └── build.go         #   build command implementation
```
