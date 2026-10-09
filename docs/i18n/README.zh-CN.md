# p4runtime-go-controller

[![CI](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml)
[![CodeQL](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml)
[![codecov](https://codecov.io/gh/zhh2001/p4runtime-go-controller/branch/main/graph/badge.svg)](https://codecov.io/gh/zhh2001/p4runtime-go-controller)
[![Go Reference](https://pkg.go.dev/badge/github.com/zhh2001/p4runtime-go-controller.svg)](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller)
[![Go Report Card](https://goreportcard.com/badge/github.com/zhh2001/p4runtime-go-controller)](https://goreportcard.com/report/github.com/zhh2001/p4runtime-go-controller)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zhh2001/p4runtime-go-controller)](../../go.mod)
[![Latest Release](https://img.shields.io/github/v/release/zhh2001/p4runtime-go-controller?sort=semver)](https://github.com/zhh2001/p4runtime-go-controller/releases/latest)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](../../LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://www.conventionalcommits.org)

面向 P4Runtime 控制器的生产级 Go SDK。

- 兼容任意 P4Runtime 1.3.0+ 目标设备（BMv2、Stratum、基于 Tofino 的交换机、自定义 ASIC 代理等）。
- 除了 `google.golang.org/grpc`、`google.golang.org/protobuf` 和官方 P4Runtime proto stubs，核心包无额外硬依赖。
- 通过 `log/slog` 输出结构化日志，应用可通过 gRPC 拦截器接入度量和链路追踪。内置 metrics 接口及适配器仍在规划中。详见 [日志与度量](../observability.md)。

> 自 `v1.0.0` 起，公共 API 遵循 [Go 1 兼容性承诺](https://go.dev/doc/go1compat)。任何破坏性变更都会记录在 [CHANGELOG](../../CHANGELOG.md) 中。

## 安装

需要 Go 1.25 或更新版本。在应用的 Go 模块目录中运行：

```sh
go get github.com/zhh2001/p4runtime-go-controller@latest
```

## 快速上手

启动项目自带的 BMv2 目标前，请按 [快速上手指南](../quickstart.md) 获取仓库并准备工具。原生模式需要本地 `p4c-bm2-ss` 和 `simple_switch_grpc`，Docker 模式也需要本地 P4 编译器。

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

更多端到端示例见 [`examples/`](../../examples) 目录，涵盖连接、管线下发、L2 学习交换机、Packet I/O 以及计数器读取。

Counter、Meter 和 Register 的读取接口只用 `-1` 表示读取整个数组，写入必须指定非负索引。普通索引会在发送请求前按 P4Info 的数组大小校验。声明了 `index_type_name` 的索引由目标在转换后检查，SDK 保留其非负原值。

Pipeline 在构造时复制 P4Info 和设备配置，`Info`、`Raw` 及资源查询返回独立副本。修改输入或查询结果不会影响后续索引、校验及管线下发。跨次查询应通过资源 ID 判断身份。需要修改管线时，编辑 `Info()` 返回的副本，再构造新的 Pipeline。构造期间不能修改输入。指针循环及异常 nil 消息会在构造阶段报错，普通类型和资源规则仍由相应 API 或目标检查。详见 [Pipeline 数据所有权](../../pipeline/README.md)。

Counter API 读写间接计数器，direct counter 使用原始 client API。写入同时发送 packets 和 bytes，由目标按声明单位处理。计数保留协议的 int64 原值，不截断为 32 位，也不将负值改为无符号十进制数。`Write(ctx, name, index, 0, 0)` 发送显式零值以清空对应计数。详见 [Counter 读写](../../counter/README.md)。

Meter 写入会按 P4Info 校验类型，速率和突发量必须非负。`EBurst` 用于单速率三色 meter。双速率要求 `PIR >= CIR`，单速率要求 `CIR = PIR`、`CBurst = PBurst`。`Write(Config{})` 发送显式全零配置，`Reset` 通过省略 Config 恢复默认 GREEN，不清空逐颜色计数器。旧 PI 使用 `-1` 表示默认行为的调用应改为 `Reset`。详见 [Meter 配置](../../meter/README.md)。

## 功能矩阵

| 能力                                                 | 状态           |
| ---------------------------------------------------- | -------------- |
| 连接管理（TLS、keepalive、重连）                     | 已就绪         |
| Mastership / 仲裁（128 位 election ID）              | 已就绪         |
| 管线配置（VERIFY / RECONCILE / COMMIT）              | 已就绪         |
| P4Info 按名索引                                      | 已就绪         |
| 表项写入（EXACT / LPM / TERNARY / RANGE / OPTIONAL） | 已就绪         |
| Counters、Meters、Registers                          | 已就绪         |
| PacketIn / PacketOut                                 | 已就绪         |
| Digest 订阅与 Ack                                    | 已就绪         |
| PRE（组播组 / 克隆会话）                             | 已就绪（v1.1） |
| 流生命周期结构化日志（`log/slog`）                   | 已就绪         |
| 内置 metrics 采集                                    | 规划中         |
| Prometheus 适配器                                    | 规划中         |
| OpenTelemetry gRPC 拦截器示例                        | 规划中         |

## 版本兼容

| 控制器版本 | P4Runtime 规范 |
| ---------- | -------------- |
| `v1.x`     | 1.3.0+         |

## 文档

- [`ARCHITECTURE.md`](../../ARCHITECTURE.md)：分层设计、数据流与架构决策。
- [`docs/quickstart.md`](../quickstart.md)：快速上手。
- [`docs/troubleshooting.md`](../troubleshooting.md)：常见问题。
- [`docs/observability.md`](../observability.md)：日志配置与采集入口。
- [`docs/glossary.md`](../glossary.md)：术语表。

## 安全

漏洞报告流程见 [SECURITY.md](../../SECURITY.md)。请不要在公共 issue 中提交可能影响在线控制器的安全问题。

## 许可证

遵循 [Apache License, Version 2.0](../../LICENSE)，第三方归属信息见 [NOTICE](../../NOTICE)。

> 翻译同步状态：2026-04-20。若发现翻译落后于英文版，请提交 PR。
