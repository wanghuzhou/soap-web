//go:build darwin && cgo

package main

// Wails 的 macOS 后端通过 Objective-C 调用 WebKit/AppKit，其中拖拽与剪贴板
// 相关代码用到了 UTType（UniformTypeIdentifiers.framework）。新版 macOS SDK
// 不再由 AppKit 隐式转出该符号，若不显式链接，链接阶段会报：
//
//	Undefined symbols for architecture arm64: "_OBJC_CLASS_$_UTType"
//
// 这里只补链接参数，不引入任何 C 代码；非 darwin 或 CGO 关闭时本文件不参与
// 编译。（下面紧邻 import "C" 的注释会被 cgo 当作 C 前导码，必须保持纯 ASCII。）

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
