---
layout: home
hero:
  name: p4runtime-go-controller
  text: 用 Go 编写 P4Runtime 控制器
  tagline: Go SDK、p4ctl CLI 和可运行的 BMv2 示例。
  actions:
    - theme: brand
      text: 快速上手
      link: /zh/quickstart
    - theme: alt
      text: 项目介绍
      link: /zh/overview
    - theme: alt
      text: GitHub
      link: https://github.com/zhh2001/p4runtime-go-controller
features:
  - title: Go SDK
    details: 通过 P4Info 中的资源名称构造表项、管理管线和处理 Packet I/O。
  - title: p4ctl CLI
    details: 在终端连接目标、安装管线，并读取表项和计数器。
  - title: BMv2 示例
    details: 使用项目自带的 P4 程序，运行连接、L2 转发、Packet I/O 和计数器示例。
---

## 从本地实验开始

使用 Go 1.26 或更新版本。[快速上手](quickstart.md)介绍 v2.0.1 的安装、BMv2 启动和基本操作。[项目介绍](../i18n/README.zh-CN.md)提供中文功能说明，完整的使用指南和 API 参考见[英文文档](../quickstart.md)。

## 协议与目标支持

协议基线为 P4Runtime 1.3.0，可选资源和新字段取决于目标实现。集成测试使用 BMv2，验证范围和已知限制见[目标支持说明](../target-support.md)。

项目采用 [Apache 2.0](../../LICENSE) 许可证。
