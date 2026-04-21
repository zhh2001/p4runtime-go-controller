# p4runtime-go-controller

[![CI](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml)
[![CodeQL](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml)
[![govulncheck](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/govulncheck.yml)
[![codecov](https://codecov.io/gh/zhh2001/p4runtime-go-controller/branch/main/graph/badge.svg)](https://codecov.io/gh/zhh2001/p4runtime-go-controller)
[![Go Reference](https://pkg.go.dev/badge/github.com/zhh2001/p4runtime-go-controller.svg)](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller)
[![Go Report Card](https://goreportcard.com/badge/github.com/zhh2001/p4runtime-go-controller)](https://goreportcard.com/report/github.com/zhh2001/p4runtime-go-controller)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zhh2001/p4runtime-go-controller)](../../go.mod)
[![Latest Release](https://img.shields.io/github/v/release/zhh2001/p4runtime-go-controller?sort=semver)](https://github.com/zhh2001/p4runtime-go-controller/releases/latest)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://www.conventionalcommits.org)

面向 P4Runtime 控制器的生产级 Go SDK。

- 兼容任意 P4Runtime 1.3.0+ 目标设备(BMv2、Stratum、基于 Tofino 的交换机、
  自定义 ASIC 代理等)。
- 除了 `google.golang.org/grpc`、`google.golang.org/protobuf`
  和官方 P4Runtime proto stubs,核心包无额外硬依赖。
- 通过 `log/slog` 输出结构化日志,度量与链路追踪均可插拔。

> 自 `v1.0.0` 起,公共 API 遵循
> [Go 1 兼容性承诺](https://go.dev/doc/go1compat);任何破坏性变更都会记录在
> [CHANGELOG](../../CHANGELOG.md) 中。

## 安装

```sh
go get github.com/zhh2001/p4runtime-go-controller@latest
```

## 快速上手

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/zhh2001/p4runtime-go-controller/client"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    c, err := client.Dial(ctx, "127.0.0.1:9559",
        client.WithDeviceID(1),
        client.WithElectionID(client.ElectionID{High: 0, Low: 1}),
        client.WithInsecure(),
    )
    if err != nil {
        log.Fatalf("dial: %v", err)
    }
    defer c.Close()

    if err := c.BecomePrimary(ctx); err != nil {
        log.Fatalf("arbitration: %v", err)
    }
    log.Println("device 1 的主控制器已选出")
}
```

更多端到端示例见 [`examples/`](../../examples) 目录,涵盖连接、管线下发、
L2 学习交换机、Packet I/O 以及计数器读取。

## 功能矩阵

| 能力 | 状态 |
| --- | --- |
| 连接管理 (TLS、keepalive、重连) | 已就绪 |
| Mastership / 仲裁 (128 位 election ID) | 已就绪 |
| 管线配置 (VERIFY / RECONCILE / COMMIT) | 已就绪 |
| P4Info 按名索引 | 已就绪 |
| 表项写入 (EXACT / LPM / TERNARY / RANGE / OPTIONAL) | 已就绪 |
| Counters、Meters、Registers | 已就绪 |
| PacketIn / PacketOut | 已就绪 |
| Digest 订阅与 Ack | 已就绪 |
| PRE(组播组 / 克隆会话) | 已就绪 (v1.1) |
| Prometheus 适配器 | 规划中 |
| OpenTelemetry gRPC 拦截器示例 | 规划中 |

## 版本兼容

| 控制器版本 | P4Runtime 规范 |
| --- | --- |
| `v1.x` | 1.3.0+ |

## 文档

- [`ARCHITECTURE.md`](../../ARCHITECTURE.md):分层设计与数据流。
- [`DESIGN_NOTES.md`](../../DESIGN_NOTES.md):架构决策与开放问题。
- [`docs/quickstart.md`](../quickstart.md):快速上手。
- [`docs/troubleshooting.md`](../troubleshooting.md):常见问题。
- [`docs/glossary.md`](../glossary.md):术语表。

## 安全

漏洞报告流程见 [SECURITY.md](../../SECURITY.md)。请不要在公共 issue 中提交
可能影响在线控制器的安全问题。

## 许可证

遵循 [Apache License, Version 2.0](../../LICENSE),第三方归属信息见
[NOTICE](../../NOTICE)。

> 翻译同步状态：2026-04-20。若发现翻译落后于英文版，请提交 PR。
