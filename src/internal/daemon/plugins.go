package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// PluginInfo 插件登记信息(M1 仅元信息,不启动进程)。
type PluginInfo struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Running bool      `json:"running"` // M4 起由插件管理器维护
}

// scanPlugins 扫描 ~/nas/plugins/ 下所有可执行文件并登记元信息。
// M1 只登记不启动;M4 接入注册协议 + 懒加载 + 反代。
func (d *Daemon) scanPlugins() ([]PluginInfo, error) {
	entries, err := os.ReadDir(d.paths.Plugins)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []PluginInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&0o111 == 0 { // 仅登记可执行文件
			continue
		}
		out = append(out, PluginInfo{
			ID:      e.Name(),
			Path:    filepath.Join(d.paths.Plugins, e.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return out, nil
}
