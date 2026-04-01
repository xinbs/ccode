# ccode

[English](./README.md) | [简体中文](./README.zh-CN.md)

`ccode` is a stripped-down open-source edition extracted from `Quick_Accesst_Claude`.

This version only does one job:

- fetch the current OpenRouter free model list
- let you choose a free model
- prompt for your OpenRouter API key when missing
- optionally save the key locally in encrypted form
- export the right Claude Code environment variables
- auto-install Claude Code if it is missing
- launch `claude`

Removed from the original private project:

- CouchDB config center
- personal cloud sync / remote config
- multi-provider switching
- release publishing / self-update logic
- any personal environment or private deployment assumptions

## Scope

This project is intentionally small. It is a single-file Go implementation (`main.go`) with no third-party runtime dependency.

## Requirements

- Go 1.22+
- an OpenRouter API key
- `bash` plus `curl` or `wget` on Linux/macOS for automatic Claude installation
- PowerShell on Windows for automatic Claude installation

## Build

```bash
go build -o ccode .
```

Multi-platform release build:

```bash
bash ./build.sh
```

That produces:

- `dist/ccode-<version>-linux-amd64`
- `dist/ccode-<version>-linux-arm64`
- `dist/ccode-<version>-darwin-amd64`
- `dist/ccode-<version>-darwin-arm64`
- `dist/ccode-<version>-windows-amd64.exe`
- `dist/ccode-<version>-windows-arm64.exe`
- `dist/SHA256SUMS.txt`

## Quick Start

1. Put your key in environment:

```bash
export OPENROUTER_API_KEY="your-key"
```

2. Launch interactive selection:

```bash
./ccode
```

On first run, if no key is configured, it will:

- tell you to sign up at `https://openrouter.ai/` and create a free API key
- prompt for your OpenRouter API key
- ask whether to save it locally
- store it in encrypted form under `~/.config/ccode-openrouter/openrouter_key.enc.json`

Then it fetches the current free models from OpenRouter, ranks them, lets you filter by prefix, and starts `claude`.

If `claude` is not installed yet, `ccode` will try to install it automatically using the official Claude installer, then continue launching it.

## Commands

Launch Claude Code with an interactively selected free model:

```bash
./ccode
```

Print shell exports instead of launching:

```bash
eval "$(./ccode env)"
```

List current free models:

```bash
./ccode models
```

List current free models as JSON:

```bash
./ccode models --json
```

Delete the saved local key:

```bash
./ccode key clear
```

Pin a specific model:

```bash
./ccode launch --model "deepseek/deepseek-r1-0528:free"
```

Pass extra args to Claude Code:

```bash
./ccode -- -p "summarize this repository"
```

Clear related env vars in the current shell:

```bash
eval "$(./ccode unset)"
```

## Config

Optional config file locations:

- `./config.json`
- `~/.config/ccode-openrouter/config.json`
- path from `CCODE_CONFIG`

Optional env file locations:

- `./.env`
- `~/.config/ccode-openrouter/ccode.env`
- path from `CCODE_ENV_FILE`

Key lookup order:

- `OPENROUTER_API_KEY`
- `CCODE_OPENROUTER_API_KEY`
- env var named by `openrouter_api_key_env`
- saved encrypted local key
- `openrouter_api_key` in config
- interactive prompt

Example config:

```json
{
  "openrouter_api_key_env": "OPENROUTER_API_KEY",
  "base_url": "https://openrouter.ai/api",
  "launch_cmd": "claude",
  "default_model": "",
  "http_referer": "https://localhost",
  "title": "ccode-openrouter"
}
```

`default_model` is only used as a fallback if the live free-model fetch fails.

## Notes

- This tool uses OpenRouter's Anthropic-compatible endpoint for Claude Code.
- If no `--model` is given and the terminal is non-interactive, `ccode` auto-picks the top ranked free model.
- On Linux/macOS, auto-install uses the official `install.sh` script and requires `bash` plus `curl` or `wget`.
- On Windows, auto-install uses the official `install.ps1` script and requires PowerShell.
- The saved key is encrypted before being written to disk and the file is created with user-only permissions.
- This is local-at-rest protection to avoid plain-text storage. It is not a full replacement for a native OS keychain.

## License

MIT
