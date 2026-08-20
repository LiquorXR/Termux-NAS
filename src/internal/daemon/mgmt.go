package daemon

import (
	"fmt"

	"github.com/termux-nas/nas/internal/mgmt"
)

// startMgmt 启动管理通道(Unix socket / 开发环境回环 TCP)。
// 仅本机可访问;只暴露生命周期方法(见 daemon.Handle)。
func (d *Daemon) startMgmt() error {
	ln, err := mgmt.Listen(d.paths.SockPath)
	if err != nil {
		return fmt.Errorf("监听管理通道: %w", err)
	}
	d.mgmtSrv = mgmt.NewServer(ln, d, d.cfg.ManageToken, d.log)
	go d.mgmtSrv.Serve()
	d.log.Info("管理通道就绪", "sock", mgmt.SocketFile(d.paths.SockPath))
	return nil
}

// stopMgmt 关闭管理通道。
func (d *Daemon) stopMgmt() {
	if d.mgmtSrv != nil {
		_ = d.mgmtSrv.Close()
	}
}
