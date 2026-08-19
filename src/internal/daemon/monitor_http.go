package daemon

import (
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/monitor"
)

// monitorSummary GET /api/monitor/summary → 系统状态 JSON(需登录)。
func (d *Daemon) monitorSummary(c *fiber.Ctx) error {
	return c.JSON(monitor.Collect(d.files.Root()))
}

// monitorPartial GET /partials/monitor → 监控看板 HTML 片段(HTMX 每 3s 自轮询)。
func (d *Daemon) monitorPartial(c *fiber.Ctx) error {
	s := monitor.Collect(d.files.Root())
	var buf strings.Builder
	if err := monitorTmpl.Execute(&buf, s); err != nil {
		d.log.Error("渲染监控看板失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).SendString("渲染失败")
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(buf.String())
}

// monitorTmpl 监控看板模板;hx-trigger="every 3s" 实现轮询自刷新。
var monitorTmpl = template.Must(template.New("monitor").Funcs(template.FuncMap{
	"humansize": humanSize,
	"uptime":    formatUptime,
}).Parse(`
<div class="card" id="monitor-card" hx-get="/partials/monitor" hx-trigger="every 3s" hx-swap="outerHTML">
  <h2>系统监控</h2>
  <div class="mgrid">
    <div class="mcell"><span>CPU</span><b>{{printf "%.1f" .CPUPercent}}%</b></div>
    <div class="mcell"><span>内存</span><b>{{printf "%.1f" .MemPercent}}% <i>{{humansize .MemUsed}} / {{humansize .MemTotal}}</i></b></div>
    <div class="mcell"><span>磁盘</span><b>{{printf "%.1f" .DiskPercent}}% <i>可用 {{humansize .DiskFree}} / {{humansize .DiskTotal}}</i></b></div>
    <div class="mcell"><span>运行时长</span><b>{{uptime .Uptime}}</b></div>
    {{if .Battery}}
    <div class="mcell"><span>电量</span><b>{{.Battery.Percentage}}%<i> {{.Battery.Status}}{{if gt .Battery.Temperature 0}} · {{printf "%.1f" .Battery.Temperature}}℃{{end}}</i></b></div>
    {{end}}
    {{if .Net}}
    <div class="mcell"><span>网络</span><b>↓{{humansize .Net.RxBytes}}<i> ↑{{humansize .Net.TxBytes}}</i></b></div>
    {{end}}
  </div>
  <p class="msub">{{.Platform}} · {{.Hostname}} · {{.Timestamp}}</p>
</div>
`))

// humanSize 人类可读字节数。
func humanSize(n uint64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatUint(n, 10) + " B"
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + []string{"KB", "MB", "GB", "TB"}[exp]
}

// formatUptime 秒数转人类可读时长。
func formatUptime(sec float64) string {
	d := time.Duration(sec * float64(time.Second))
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(m) + "m" + strconv.Itoa(s) + "s"
}
