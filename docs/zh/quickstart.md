# 快速上手

本页使用 v2.0.1 和项目自带的 L2 P4 程序。Go 需要 1.26 或更新版本，原生 BMv2 模式需要 Bash、`p4c-bm2-ss` 和 `simple_switch_grpc`。

## 1. 安装 SDK 和 CLI

在应用的 Go 模块目录中安装 SDK：

```sh
go get github.com/zhh2001/p4runtime-go-controller/v2@v2.0.1
```

新应用先创建目录并运行 `go mod init example.com/p4-controller`。CLI 可以单独安装：

```sh
go install github.com/zhh2001/p4runtime-go-controller/v2/cmd/p4ctl@v2.0.1
```

将 Go 的 `GOBIN` 目录加入 `PATH`。未设置 `GOBIN` 时，安装目录为 `GOPATH/bin`。SDK 的导入路径包含 `/v2`，CLI 命令仍叫 `p4ctl`。

## 2. 启动 BMv2

获取对应版本的仓库：

```sh
git clone https://github.com/zhh2001/p4runtime-go-controller.git
cd p4runtime-go-controller
git checkout v2.0.1
./scripts/run-bmv2.sh
```

脚本会同时编译 L2 P4Info 和 BMv2 JSON，随后在前台启动目标。默认设备 ID 为 1，gRPC 地址为 `127.0.0.1:9559`，CPU 口为 255。保留此终端。

Docker 模式使用 `./scripts/run-bmv2.sh --docker`，仍需本地 P4 编译器。自定义端口和接口绑定见[启动脚本说明](../../scripts/README.md)。

## 3. 安装管线并写入表项

在另一个终端进入同一仓库目录，运行：

```sh
go run ./examples/02_l2_switch \
  --addr 127.0.0.1:9559 \
  --p4info ./examples/testdata/l2.p4info.txt \
  --config ./examples/testdata/l2.bmv2.json
```

示例安装管线，并写入一条固定表项，将目的 MAC 为 `00:11:22:33:44:55` 的帧转发到端口 1。正常日志包含：

```text
pipeline installed via VERIFY_AND_COMMIT
wrote 1 entry
```

以太网转发需要给数据端口绑定主机接口。接口设置和控制头格式见 [L2 实验说明](../../examples/testdata/README.md)。

## 4. 收发 CPU 数据包并读取计数器

管线安装后，运行 CPU 口回环示例：

```sh
go run ./examples/03_packetio \
  --addr 127.0.0.1:9559 \
  --p4info ./examples/testdata/l2.p4info.txt \
  --send-port 255
```

它通过 gRPC 发送 PacketOut，并打印回环得到的 PacketIn。该操作不需要主机数据接口，用 Ctrl+C 退出。

读取端口 1 对应的间接计数器：

```sh
p4ctl counter read \
  --p4info ./examples/testdata/l2.p4info.txt \
  --counter MyIngress.pkt_counter --index 1
```

计数来自经过 L2 转发 action 的流量。得到非零结果需要从已绑定的数据接口发送匹配流量，CPU 回环不能替代端口 1 的转发流量。

## 继续使用

[英文快速开始](../quickstart.md)提供完整的 Go 控制器代码。[CLI 指南](../../cmd/p4ctl/README.md)介绍表项操作、配置文件和 JSON/YAML 输出。[示例列表](../examples.md)说明四个示例的用途。
