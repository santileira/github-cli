# GitHub CLI PR Status Tool

A command-line tool to monitor GitHub Pull Request status with interactive merge capabilities.

## Features

- 📊 Display detailed PR status including reviews, checks, and merge readiness
- 🔄 Watch mode with auto-refresh every minute
- 🔔 Desktop notifications (macOS) when PR is ready to merge
- 📝 Interactive ready - type `ready` while watching to convert draft PR to ready for review
- ⚡ Interactive merge - type `merge` while watching to squash and merge
- ✅ Automatic verification of merge requirements
- 🎨 Colored output with clickable links (in supported terminals)

## Requirements

- Go 1.x or higher
- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated
- macOS (for desktop notifications)

## Installation

```bash
# Clone the repository
git clone https://github.com/santileira/github-cli.git
cd github-cli

# Build the binary
go build -o gh-cli main.go
```

## Usage

### Check PR Status

```bash
./gh-cli status <owner/repo> --pr <pr-number>
```

Example:
```bash
./gh-cli status embrace-io/devops --pr 10646
```

### Watch Mode with Interactive Commands

```bash
./gh-cli status <owner/repo> --pr <pr-number> --watch
```

In watch mode:
- The status refreshes every minute
- You'll get a notification when the PR is ready to merge
- Type `ready` and press Enter to mark a draft PR as ready for review
- Type `merge` and press Enter to merge the PR
- The tool will verify readiness and perform a squash merge with auto-confirm

Example:
```bash
./gh-cli status embrace-io/devops --pr 10646 --watch
```

### List PRs by Author

```bash
./gh-cli status <owner/repo> --author <username>
```

## Interactive Commands

### Ready Command

When you type `ready` in watch mode (for draft PRs), the tool:

1. ✅ Verifies the PR is in draft mode
2. 📝 Marks the PR as ready for review using `gh pr ready`

If the PR is already ready for review, it will show a warning.

### Merge Command

When you type `merge` in watch mode, the tool:

1. ✅ Verifies the PR is ready (checks passing, approvals, clean merge state)
2. 🔀 Performs a squash merge (`--squash`)
3. 🤖 Auto-confirms without interactive editing (`--auto`)
4. 🗑️ Deletes the branch after merge (`--delete-branch`)

If the PR is not ready, it will show you what's missing:
- Changes requested by reviewers
- Missing required approvals
- Failing checks
- Mergeable state issues

## Authentication

The tool uses GitHub CLI for authentication. Make sure you're logged in:

```bash
gh auth login
```

Alternatively, set the `GH_TOKEN` environment variable:

```bash
export GH_TOKEN=your_github_token
```

## Development

### Dependencies

- [github.com/spf13/cobra](https://github.com/spf13/cobra) - CLI framework
- [github.com/fatih/color](https://github.com/fatih/color) - Colored terminal output

Install dependencies:
```bash
go mod download
```

## License

MIT
