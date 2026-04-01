# ccode

[English](./README.md) | [简体中文](./README.zh-CN.md)

`ccode` 是从 `Quick_Accesst_Claude` 中拆出来的精简开源版。

这个版本只做一件事：

- 动态获取 OpenRouter 当前可用的免费模型
- 交互选择免费模型
- 当缺少 OpenRouter API Key 时提示输入
- 可选地将 Key 加密保存到本地
- 设置 Claude Code 所需环境变量
- 如果本机未安装 Claude Code，自动调用官方安装脚本安装
- 最后自动启动 `claude`

从原来的私有项目中移除的内容：

- CouchDB 配置中心
- 个人云端同步 / 远程配置
- 多厂商切换
- 发布 / 自更新逻辑
- 任何个人化或私有部署耦合

## 项目范围

这个项目刻意保持很小，只保留一个 Go 源文件：[main.go](/home/moltbot/code/ccode/main.go)，没有第三方运行时依赖。

## 运行要求

- Go 1.22+
- OpenRouter API Key
- Linux/macOS 下如需自动安装 Claude Code，需要 `bash` 和 `curl` 或 `wget`
- Windows 下如需自动安装 Claude Code，需要 PowerShell

## 构建

本地构建：

```bash
go build -o ccode .
```

多平台打包：

```bash
bash ./build.sh
```

会生成：

- `dist/ccode-<version>-linux-amd64`
- `dist/ccode-<version>-linux-arm64`
- `dist/ccode-<version>-darwin-amd64`
- `dist/ccode-<version>-darwin-arm64`
- `dist/ccode-<version>-windows-amd64.exe`
- `dist/ccode-<version>-windows-arm64.exe`
- `dist/SHA256SUMS.txt`

## 快速开始

直接运行：

```bash
./ccode
```

首次运行时，如果还没有配置 Key，它会：

- 提示先到 `https://openrouter.ai/` 注册账号并创建一个免费的 API Key
- 提示输入 OpenRouter API Key
- 询问是否保存到本地
- 将 Key 以加密形式保存到 `~/.config/ccode-openrouter/openrouter_key.enc.json`

之后它会：

- 拉取 OpenRouter 当前免费模型列表
- 按热度 / 新旧 / 能力进行排序
- 支持通过前缀过滤模型
- 选定模型后自动启动 `claude`

如果系统中还没有安装 `claude`，`ccode` 会自动调用 Anthropic 官方安装脚本，安装完成后继续启动。

## 常用命令

交互选择模型并启动 Claude Code：

```bash
./ccode
```

仅输出环境变量，不直接启动：

```bash
eval "$(./ccode env)"
```

查看当前免费模型列表：

```bash
./ccode models
```

以 JSON 形式输出免费模型列表：

```bash
./ccode models --json
```

删除本地已保存的 Key：

```bash
./ccode key clear
```

指定模型直接启动：

```bash
./ccode launch --model "deepseek/deepseek-r1-0528:free"
```

给 Claude Code 追加参数：

```bash
./ccode -- -p "summarize this repository"
```

清除当前 shell 里的相关环境变量：

```bash
eval "$(./ccode unset)"
```

## 配置

可选配置文件位置：

- `./config.json`
- `~/.config/ccode-openrouter/config.json`
- 通过 `CCODE_CONFIG` 指定的路径

可选环境文件位置：

- `./.env`
- `~/.config/ccode-openrouter/ccode.env`
- 通过 `CCODE_ENV_FILE` 指定的路径

Key 查找顺序：

- `OPENROUTER_API_KEY`
- `CCODE_OPENROUTER_API_KEY`
- `openrouter_api_key_env` 指向的环境变量
- 本地加密保存的 Key
- 配置文件中的 `openrouter_api_key`
- 交互输入

配置示例：

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

其中 `default_model` 仅在实时拉取免费模型失败时作为回退值使用。

## 说明

- 本工具通过 OpenRouter 的 Anthropic 兼容接口驱动 Claude Code
- 如果没有显式传 `--model`，且当前终端是非交互环境，会自动选择排序后的第一个免费模型
- Linux/macOS 下自动安装 Claude Code 走官方 `install.sh`
- Windows 下自动安装 Claude Code 走官方 `install.ps1`
- 保存到本地的 Key 会先加密，再以仅当前用户可读写的权限写盘
- 这种方式主要是避免明文落盘，不等同于操作系统原生 Keychain / Credential Manager

## License

MIT
