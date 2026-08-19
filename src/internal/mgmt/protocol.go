// Package mgmt 实现管理通道:nasm ↔ nasd 的 JSON-RPC over local socket。
//
// 严格遵循开发文档 §2.2/§4.2:管理通道仅监听本机,只暴露主框架生命周期方法,
// 不暴露任何插件操作(插件操作一律走用户通道 HTTP API,需登录)。
package mgmt

import "encoding/json"

// 支持的管理方法名。
const (
	MethodStatus      = "daemon.status"      // 运行状态/版本/uptime/健康
	MethodStop        = "daemon.stop"        // 优雅停止(先停插件,再退出)
	MethodEnterUpdate = "daemon.enterUpdate" // 进入更新模式(暂停服务等待替换)
	MethodLogTail     = "log.tail"           // 主框架日志尾部
)

// Request JSON-RPC 请求。
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	// Auth 管理 token(socket 仅本机可访问,此处为第二道防线)。
	Auth string `json:"auth,omitempty"`
	ID   int64  `json:"id"`
}

// Response JSON-RPC 响应。
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
	ID     int64           `json:"id"`
}

// RPCError 结构化错误。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error 实现 error 接口。
func (e *RPCError) Error() string {
	return e.Message
}

// 错误码。
const (
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// StatusResult daemon.status 返回值。
type StatusResult struct {
	Running bool   `json:"running"`
	Version string `json:"version"`
	Uptime  int64  `json:"uptime"` // 秒
	PID     int    `json:"pid"`
	Healthy bool   `json:"healthy"`
	Port    int    `json:"port"`
}

// LogTailParams log.tail 参数。
type LogTailParams struct {
	Lines int `json:"lines"`
}

// LogTailResult log.tail 返回值。
type LogTailResult struct {
	Lines []string `json:"lines"`
}
