# Installation Guide

This guide covers the steps to install and configure Scion on your local machine.

## Prerequisites

### 1. Go
Scion is written in Go. You need Go 1.22 or later installed.
- [Download and install Go](https://golang.org/doc/install)

### 2. Container Runtime
Scion requires a container runtime to manage agents. You can use either Docker or the Apple Virtualization Framework (experimental).

#### Docker (Linux/Windows)
- Install [Docker Desktop](https://www.docker.com/products/docker-desktop) or [Docker Engine](https://docs.docker.com/engine/install/).
- Ensure the `docker` command is available in your PATH.

#### Apple Virtualization (macOS only)
- Requires the [container](https://github.com/apple/container/releases) tool (an Apple tool for running OCI images in micro VMs).
- Ensure the `container` command executes.
- Although the container images refrenced in this project are public, the container tool seems to require auth, until this can be investigated, this will authorized the container tool to pull images from the GCP Artifact registry (requires gcloud) `gcloud auth print-access-token | container registry login --username oauth2accesstoken --password-stdin us-central1-docker.pkg.dev`

### 3. Git
Scion uses `git worktree` to manage agent workspaces.
- Ensure `git` is installed and available in your PATH.
- Because Scion uses a new feature for relative path worktrees ensure that `git --version` >= 2.48

---

## Scion Installation

### From Source
You can install Scion directly using `go install`:

```bash
go install github.com/ptone/scion-agent@latest
```

Ensure your `$GOPATH/bin` (typically `~/go/bin`) is in your system `$PATH`.

### From Binary
If you have the repository cloned, you can use the provided `Makefile`:

```bash
make build
# This creates a 'scion' binary in the current directory.
# You can move it to a directory in your PATH:
mv scion /usr/local/bin/
```

To verify your installation, run:

```bash
scion version
```

---

## Configuration

### 1. Initialize a Grove
Navigate to the root of a project where you want to use Scion and run:

```bash
scion init
```

This creates a `.scion` directory in your project root containing:
- `settings.json`: Grove-specific settings.
- `templates/`: Default agent templates (gemini, claude, etc.).

### 2. Select Runtime
Scion automatically selects the appropriate runtime based on your operating system:
- **macOS**: Defaults to `container` (Apple Virtualization Framework).
- **Linux/Windows**: Defaults to `docker`.

If you wish to change this (e.g., to use Docker on macOS), you can manually edit `.scion/settings.json`:

```json
{
  "profiles": {
    "local": {
      "runtime": "docker"
    }
  }
}
```

---



---

## Troubleshooting

### `git worktree` errors
Ensure your project is a git repository. `scion init` and `scion start` require being inside a git repository to manage workspaces.

### Permission Denied (Docker)
Ensure your user has permissions to run Docker commands without `sudo`. On Linux, add your user to the `docker` group.

### Path Issues
If `scion` command is not found after `go install`, add the following to your shell profile (`.zshrc` or `.bashrc`):

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```
