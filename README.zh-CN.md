# Buildkite Agent

[English](README.md) | [中文](README.zh-CN.md)

![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=main)
[![Go Reference](https://pkg.go.dev/badge/github.com/buildkite/agent/v3.svg)](https://pkg.go.dev/github.com/buildkite/agent/v3)

Buildkite Agent 是一个小型、可靠且跨平台的构建工具，能够帮助你在自有基础设施上轻松运行自动化构建任务。它的主要职责包括：从 buildkite.com 获取工作列表、执行构建任务、报告任务的状态码和输出日志，以及将任务的结果文件上传到指定的位置。

完整文档请参见 [buildkite.com/docs/agent](https://buildkite.com/docs/agent)。

```text
$ buildkite-agent --help
Usage:

  buildkite-agent <command> [options...]

Available commands are:

  acknowledgements  Prints the licenses and notices of open source software incorporated into this software.
  start             Starts a Buildkite agent
  annotate          Annotate the build page within the Buildkite UI with text from within a Buildkite job
  annotation        Make changes to an annotation on the currently running build
  artifact          Upload/download artifacts from Buildkite jobs
  env               Process environment subcommands
  lock              Process lock subcommands
  meta-data         Get/set data from Buildkite jobs
  oidc              Interact with Buildkite OpenID Connect (OIDC)
  pipeline          Make changes to the pipeline of the currently running build
  step              Get or update an attribute of a build step
  bootstrap         Run a Buildkite job locally
  help              Shows a list of commands or help for one command

Use "buildkite-agent <command> --help" for more information about a command.
```

## 依赖

这个 Agent 在大多数受支持的平台上无需额外配置即可运行。 在 Linux 上，只需要使用 `dbus`计科。

## 安装

Buildkite 的[代理页面](https://buildkite.com/organizations/-/agents)提供了个性化安装说明，或者也可以参考 [Buildkite 文档](https://buildkite.com/docs/agent/self-hosted/install)。这些文档涵盖了在 Ubuntu 系统上通过 apt 软件包安装代理、在 Debian 系统上通过 apt 软件包安装代理、在 macOS 系统上通过 Homebrew 安装代理，以及在使用 Windows 和 Linux 系统上的安装方法。

## Docker

我们也支持并发布了以下操作系统的 [Docker 镜像](https://hub.docker.com/r/buildkite/agent)。Docker 镜像使用 Agent 的语义化版本号以及操作系统进行标记。

例如，Agent 版本 3.45.6 会发布为：

- 在 Ubuntu 20.04版本中，会跟踪与版本 3 相关的次要更新和错误修复。
- 在 Ubuntu 20.04 版本中， 3.45版本的补丁修复更新已被安装。

* 3.45.6-ubuntu-20.04对应的是在 Ubuntu 20.04 中已安装的特定版本。

#### 支持的操作系统

- Alpine 3.18
- Ubuntu 20.04 LTS (x86_64)，支持至 20.04 版本末。
- Ubuntu 22.04 LTS (x86_64)，支持到 22.04 版本末
- Ubuntu 24.04 LTS (x86_64)，支持到 24.04 的版本末

## 启动

启动 Agent 只需要你的 Agent token（可在 Buildkite 的 Agents 页面中找到），还有一个构建路径。例如：

```bash
buildkite-agent start --token=<your token> --build-path=/tmp/buildkite-builds
```

### 遥测

默认情况下，Agent 会向 Buildkite 的主服务器发送一些关于当前使用功能的信息。不会发送敏感或可识别身份的信息。如果您希望禁用这个功能报告，可以在 `buildkite-agent start` 调用中添加 `--no-feature-reporting` 参数。我们跟踪的功能可以在 [AgentStartConfig.Features](https://github.com/search?q=repo%3Abuildkite%2Fagent+language%3Ago+symbol%3AAgentStartConfig.Features+&type=code) 中查看。

## 开发

以下说明假定您使用的是最新版本的 macOS 操作系统，但也可以轻松适配到 Linux 和 Windows。

```bash
# 确认已经安装 Go。
brew install go

# 下载代码到某个地方 - 不需要 GOPATH。
git clone https://github.com/buildkite/agent.git
cd agent

# 创建临时构建目录。
mkdir /tmp/buildkite-builds

# 构建 Agent 二进制文件并启动 Agent。
go build -o /usr/local/bin/buildkite-agent .
buildkite-agent start --debug --build-path=/tmp/buildkite-builds --token "abc"

# 或者，直接运行 Agent 并跳过构建步骤。
go run *.go start --debug --build-path=/tmp/buildkite-builds --token "abc"
```

### Go 版本与依赖管理

最新版本的 Agent 通常使用最新稳定版 Go 编译。之前的 Go 版本也可能能够运行，但无法保证完全兼容。由于我们现在使用的是一些较新的语言特性，比如泛型编程，因此使用低于 1.18 版本的 Go 语言进行编译会导致编译失败。

我们使用 [Go Modules](https://github.com/golang/go/wiki/Modules) 来管理依赖。除非必要，否则不会将依赖项直接 [包含](https://go.dev/ref/mod#go-mod-vendor) 在仓库中。

这个仓库发布的 Go 模块（即那些可以通过在代码中添加 `import "github.com/buildkite/agent/v3"` 使用的模块）并不采用语义版本控制来管理版本。在次要版本中可能会引入一些重大的变更。请自行承担将代理作为您的 Go 应用程序运行时依赖项的风险。

## 平台支持

我们仅对当前主版本提供安全性和 bug 修复支持。

我们的架构和操作系统支持主要受 [Go 本身支持范围](https://github.com/golang/go/wiki/MinimumRequirements) 的限制。

### 架构支持

我们提供以下机器架构支持（灵感来自 Rust 语言平台支持指南）。

##### Tier 1，保证可用

- linux x86_64
- linux arm64
- windows x86_64

##### Tier 2，保证可构建

- linux x86
- windows x86
- darwin x86_64
- darwin arm64

##### Tier 3，由社区成员共同维护

我们会发布各种其他平台的二进制文件，任何 Go 支持的平台都应可以构建 Agent，但官方不提供对这些 Tier 3 平台的支持。

### 操作系统支持

目前，我们支持在以下操作系统上运行 Buildkite 代理程序：这些操作系统在未来的一些次要版本更新中可能会失去支持，因为这些操作系统可能不再受到最新稳定版本的 Go 语言的支持。

这个代理程序具有很好的可移植性，应该在大多数类似 UNIX 的系统以及 Windows 操作系统上正常运行。

- Ubuntu 20.04 及更新版本
- Debian 8 及更新版本
- Red Hat RHEL 7 及更新版本
- CentOS
  - CentOS 7
  - CentOS 8
- Amazon Linux 2
- macOS [^1]
  - 12 (Monterey)
  - 13 (Ventura)
  - 14 (Sonoma)
  - 15 (Sequoia)
  - 26 (Tahoe)
- Windows [^2]
  - 10
  - 11
  - Server 2016
  - Server 2019
  - Server 2022

## 贡献

参见 [./CONTRIBUTING.md](./CONTRIBUTING.md)

## 贡献者

非常感谢
[我们的优秀贡献者](https://github.com/buildkite/agent/graphs/contributors)！你们都很棒，我们由衷感谢你们 ❤️

## 版权

Copyright (c) 2014-2023 Buildkite Pty Ltd.
See [LICENSE](./LICENSE.txt) for details.

[^1]: 参见 https://github.com/golang/go/issues/23011 关于 macOS / Go 支持的问题，以及 [Supported macOS Versions](./docs/macos.md) 查看早于上述版本的 Buildkite Agent 最后支持版本。
    
[^2]: 参见 [Go 的 Windows 支持页面](https://go.dev/wiki/Windows) 了解 Go / Windows 兼容性，以及 [Supported Windows Versions](./docs/windows.md) 查看早于上述版本的 Buildkite Agent 最后支持版本。
