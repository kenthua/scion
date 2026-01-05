# Scion

Scion is a container-based orchestration tool designed to manage concurrent LLM-based code agents across your local machine and remote clusters. It enables developers to run specialized sub-agents with isolated identities, credentials, and workspaces, allowing for parallel execution of tasks such as coding, auditing, and testing.

## Key Features

- **Parallelism**: Run multiple agents concurrently as independent processes either locally or remote.
- **Isolation**: Each agent runs in its own container with strict separation of credentials, configuration, and environment.
- **Context Management**: Scion uses `git worktree` to provide each agent with a dedicated workspace, preventing merge conflicts and ensuring clean separation of concerns.
- **Specialization**: Agents can be customized via [Templates](docs/guides/templates.md) (e.g., "Security Auditor", "QA Tester") to perform specific roles.
- **Interactivity**: Agents support "detached" background operation, but users can "attach" to any running agent for human-in-the-loop interaction.
- **Multi-Runtime**: Supports Docker, Apple Virtualization Framework, and (Experimental) Kubernetes.

## Documentation

- **[Concepts](docs/concepts.md)**: Understanding Agents, Groves, Harnesses, and Runtimes.
- **[Installation](docs/install.md)**: How to get Scion up and running.
- **[CLI Reference](docs/reference/cli.md)**: Comprehensive guide to all Scion commands.
- **[Configuration Reference](docs/scion-config-reference.md)**: Detailed `scion-agent.json` options.
- **Guides**:
    - [Using Templates](docs/guides/templates.md)
    - [Kubernetes Runtime](docs/guides/kubernetes.md)

## Installation

See the **[Installation Guide](docs/install.md)** for detailed instructions.

Quick start from source:
```bash
go install github.com/ptone/scion-agent@latest
```

## Quick Start

### 1. Initialize a Grove

Navigate to your project root and initialize a new Scion grove. This creates the `.scion` directory and seeds default templates.

```bash
cd my-project
scion init
```

Note: Scion automatically detects your operating system and configures the default runtime (Docker for Linux/Windows, Container for macOS). You can change this in `.scion/settings.json`.

### 2. Start Agents

You can launch an agent immediately using `start` (or its alias `run`). By default, this runs in the background using the `gemini` template.

```bash
# Start a gemini agent named "coder"
scion start coder "Refactor the authentication middleware in pkg/auth"

# Start a Claude-based agent
scion run auditor "Audit the user input validation" --type claude

# Start and immediately attach to the session
scion start debug "Help me debug this error" --attach
```

### 3. Manage Agents

- **List active agents**: `scion list`
- **Attach to an agent**: `scion attach <agent-name>`
- **View logs**: `scion logs <agent-name>`
- **Stop an agent**: `scion stop <agent-name>`
- **Resume an agent**: `scion resume <agent-name>`
- **Delete an agent**: `scion delete <agent-name>` (removes container, directory, and worktree)

## Configuration

Scion settings are managed in `settings.json` files, following a precedence order: **Grove** (`.scion/settings.json`) > **Global** (`~/.scion/settings.json`) > **Defaults**.

Templates serve as blueprints and can be managed via the `templates` subcommand. See the [Templates Guide](docs/guides/templates.md) for more details.

## License

This project is licensed under the Apache License, Version 2.0. See the [LICENSE](LICENSE) file for details.