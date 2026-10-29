# TraceX CLI

Ask TraceX about your data, code and recent changes from the terminal.

```sh
curl -fsSL https://app.tracexpro.com/install | sh
```

The installer downloads the release for your system, checks its SHA-256
against the release's `checksums.txt`, and puts `tracex` in `~/.local/bin`
(`TRACEX_INSTALL_DIR` changes that; `TRACEX_VERSION=v0.2.0` pins a release).
On Windows, download `tracex_windows_amd64.zip` from the releases page.

## Commands

| Command | What it does |
| --- | --- |
| `tracex login` | Signs in through your browser. TraceX shows a code; you approve it in the dashboard, with your usual SSO or two-factor sign-in. The CLI never sees your password. |
| `tracex init` | Run inside a git repository. Picks the TraceX project, connects the repository through the TraceX GitHub App if it isn't connected yet, and writes `.tracex.json`. |
| `tracex -c` | Opens a chat session, like Conversations in the dashboard. |
| `tracex -c "question"` | Answers one question and exits. Piped input works too: `git diff \| tracex -c`. |
| `tracex status` | Shows who is signed in, the server, and the project this folder uses. |
| `tracex logout` | Ends the CLI's session on the server and removes it from this computer. |
| `tracex version` | Prints the version. |

In a chat session:

| Input | What it does |
| --- | --- |
| `/new` | Starts a new conversation. |
| `/project` | Switches project. |
| `/open` | Opens the conversation in the dashboard. |
| `/exit` or ctrl+d | Quits. |
| ctrl+c | Stops an answer. |
| `\` at the end of a line | Continues the question on the next line. |

## Where things are kept

- **`~/.tracex/credentials.json`** (mode 0600): the CLI's session. It lasts 30 days and is listed under Profile → Sessions in the dashboard, where you can revoke it.
- **`.tracex.json`** in the repository: the linked project, repository and branch. It holds no secrets, so commit it and your whole team asks the same project.
- **`~/.tracex/history`**: your previous questions, for the up arrow.

The CLI talks to `https://api.tracexpro.com`. `TRACEX_API_URL` points the CLI at another server, for example `http://127.0.0.1:8000` for a local backend. `NO_COLOR` turns colors off.

## Development

```sh
go test -race ./...
go build -o tracex .
```

Pushing a `v*` tag builds the binaries with GoReleaser and publishes them as a GitHub release (`.github/workflows/release.yml`). The installer reads `releases/latest/download/tracex_<os>_<arch>.tar.gz`, so the releases must be public.
