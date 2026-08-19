package mgmt

import (
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
)

// Client 管理通道客户端(nasm 侧),每次连接处理一个调用。
type Client struct {
	conn  net.Conn
	token string
	id    atomic.Int64
	dec   *json.Decoder
	enc   *json.Encoder
}

// NewClient 连接管理通道并创建客户端。
func NewClient(sockPath, token string) (*Client, error) {
	conn, err := Dial(sockPath)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:  conn,
		token: token,
		dec:   json.NewDecoder(conn),
		enc:   json.NewEncoder(conn),
	}, nil
}

// Call 发起一次 JSON-RPC 调用,result 可为 nil(忽略返回值)。
func (c *Client) Call(method string, params, result any) error {
	id := c.id.Add(1)
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("序列化参数: %w", err)
		}
		raw = b
	}
	req := Request{Method: method, Params: raw, Auth: c.token, ID: id}
	if err := c.enc.Encode(req); err != nil {
		return fmt.Errorf("发送管理请求: %w", err)
	}
	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return fmt.Errorf("读取管理响应: %w", err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

// Close 关闭连接。
func (c *Client) Close() error { return c.conn.Close() }
