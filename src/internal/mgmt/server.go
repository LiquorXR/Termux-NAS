package mgmt

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
)

// Handler 管理方法处理器(nasd 侧实现)。
type Handler interface {
	// Handle 分发请求;返回结果 JSON 或结构化错误。
	Handle(method string, params json.RawMessage) (json.RawMessage, *RPCError)
}

// Server 管理通道服务端,循环接受连接并按请求应答。
type Server struct {
	ln    net.Listener
	h     Handler
	token string // 管理 token;为空则不校验
	log   *slog.Logger

	wg sync.WaitGroup
}

// NewServer 创建管理服务端。
func NewServer(ln net.Listener, h Handler, token string, log *slog.Logger) *Server {
	return &Server{ln: ln, h: h, token: token, log: log}
}

// Serve 阻塞式接受连接,直到 listener 关闭。
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.log.Warn("管理通道 accept 失败", "err", err)
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			s.handleConn(conn)
		}()
	}
}

// Close 关闭 listener。不等待在途连接:
// 调用场景是进程退出流程(daemon.stop/enterUpdate),等待会导致
// nasm 侧锁循环与 nasd 侧连接处理互相等待(死锁)。进程退出时
// 操作系统自动关闭所有连接句柄,在途 goroutine 随进程结束。
func (s *Server) Close() error {
	return s.ln.Close()
}

func (s *Server) handleConn(conn net.Conn) {
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Debug("管理连接解码结束", "err", err)
			}
			return
		}
		resp := s.dispatch(req)
		if err := enc.Encode(resp); err != nil {
			s.log.Warn("管理响应写入失败", "err", err)
			return
		}
	}
}

func (s *Server) dispatch(req Request) Response {
	// 鉴权:socket 仅本机可访问,管理 token 为第二道防线。
	// 恒定时间比较,避免时序侧信道泄露 token。
	if s.token != "" && subtle.ConstantTimeCompare([]byte(req.Auth), []byte(s.token)) != 1 {
		return Response{Error: &RPCError{Code: -32001, Message: "管理 token 无效"}, ID: req.ID}
	}
	result, rpcErr := s.h.Handle(req.Method, req.Params)
	return Response{Result: result, Error: rpcErr, ID: req.ID}
}
