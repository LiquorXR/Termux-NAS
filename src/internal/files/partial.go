package files

import (
	"encoding/json"
	"html/template"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// viewData 文件列表片段渲染数据。
type viewData struct {
	Path     string // 当前相对路径("/" 表示根)
	Parent   string // 上级相对路径("" 表示根)
	Query    string // 搜索词(搜索模式)
	IsSearch bool
	Entries  []entryView
	Crumbs   []crumb
}

type crumb struct {
	Label, Path string
}

type entryView struct {
	Name, Size, ModTime, HxVals string
	RelPath                     string // 相对路径(原始,供模板 urlquery)
	IsDir                       bool
	Href                        string // 导航/下载 URL(已 URL 编码)
}

// HandlePartial GET /partials/files?path=&q= → 文件管理面板 HTML 片段(需登录)。
func (s *Store) HandlePartial(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q", ""))
	rel := c.Query("path", "")
	view := viewData{Path: "/"}

	if q != "" {
		// 搜索模式
		results, err := s.search(q)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("搜索失败")
		}
		view.IsSearch = true
		view.Query = q
		view.Path = "/"
		for _, e := range results {
			view.Entries = append(view.Entries, toView(s, e))
		}
	} else {
		entries, err := s.list(rel)
		if err != nil {
			return s.respondErr(c, err)
		}
		view.Path = normDisplay(rel)
		view.Parent = parentOf(rel)
		view.Crumbs = buildCrumbs(rel)
		for _, e := range entries {
			view.Entries = append(view.Entries, toView(s, e))
		}
	}

	var buf strings.Builder
	if err := listTmpl.Execute(&buf, view); err != nil {
		s.log.Error("渲染文件列表失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).SendString("渲染失败")
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(buf.String())
}

// toView 转换条目为渲染视图。
func toView(s *Store, e Entry) entryView {
	rel := e.Path
	href := ""
	if e.IsDir {
		href = "/partials/files?path=" + url.QueryEscape(rel)
	} else {
		href = "/api/files/download?path=" + url.QueryEscape(rel)
	}
	return entryView{
		Name:    e.Name,
		RelPath: rel,
		IsDir:   e.IsDir,
		Size:    sizeText(e.Size, e.IsDir),
		ModTime: e.ModTime,
		Href:    href,
		HxVals:  `{"path":` + strconv.Quote(rel) + `}`,
	}
}

// search 复用 API 搜索逻辑(内部调用)。
func (s *Store) search(q string) ([]Entry, error) {
	var out []Entry
	entries, err := s.searchAll(q)
	if err != nil {
		return nil, err
	}
	out = append(out, entries...)
	return out, nil
}

// normDisplay 将相对路径转为前端显示格式("/" 表示根)。
func normDisplay(rel string) string {
	if rel == "" || rel == "/" {
		return "/"
	}
	return "/" + strings.TrimPrefix(filepath.ToSlash(filepath.Clean(rel)), "/")
}

// parentOf 返回上级相对路径("" 表示根)。
func parentOf(rel string) string {
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return ""
	}
	dir := filepath.Dir(filepath.FromSlash(rel))
	if dir == "." || dir == "/" {
		return ""
	}
	return filepath.ToSlash(dir)
}

// buildCrumbs 构建面包屑导航。
func buildCrumbs(rel string) []crumb {
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return []crumb{{Label: "根目录", Path: "/"}}
	}
	parts := strings.Split(rel, "/")
	crumbs := []crumb{{Label: "根目录", Path: "/"}}
	cur := ""
	for _, p := range parts {
		if cur == "" {
			cur = p
		} else {
			cur = cur + "/" + p
		}
		crumbs = append(crumbs, crumb{Label: p, Path: "/" + cur})
	}
	return crumbs
}

// sizeText 人类可读大小。
func sizeText(n int64, isDir bool) string {
	if isDir {
		return "-"
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + []string{"KB", "MB", "GB", "TB"}[exp]
}

// listTmpl 文件面板 HTML 模板。所有动态值经 html/template 自动转义。
var listTmpl = template.Must(template.New("list").Funcs(template.FuncMap{
	"jsonquote": func(s string) string {
		b, _ := json.Marshal(s)
		return string(b)
	},
}).Parse(`
<div id="files-panel" data-path="{{.Path}}">
  <div class="card">
    <div class="fbar">
      <h2>文件管理</h2>
      <div class="fsearch">
        <input name="q" placeholder="搜索文件名..." value="{{.Query}}"
               hx-get="/partials/files" hx-trigger="input changed delay:400ms"
               hx-target="#files-panel" hx-swap="outerHTML" hx-include="this">
      </div>
    </div>
    <div class="fcrumbs">
      {{if .IsSearch}}
        <a hx-get="/partials/files?path={{.Path}}" hx-target="#content">← 返回文件列表</a>
        <span class="fcount">搜索结果: {{len .Entries}} 条</span>
      {{else}}
        {{range .Crumbs}}
          {{if eq .Path $.Path}}<span class="ccur">{{.Label}}</span>{{else}}<a hx-get="/partials/files?path={{urlquery .Path}}" hx-target="#content">{{.Label}}</a><span class="csep">/</span>{{end}}
        {{end}}
      {{end}}
    </div>

    {{if not .IsSearch}}
    <div class="ftools">
      <button class="btn-mini" hx-prompt="新目录名" hx-post="/api/files/mkdir"
              hx-vals='{"path":{{jsonquote .Path}}}' hx-swap="none"
              hx-on::after-request="if(event.detail.successful) refreshFiles()">新建文件夹</button>
      <form class="fupload" hx-post="/api/files/upload" hx-encoding="multipart/form-data" hx-swap="none"
            hx-on::after-request="if(event.detail.successful) refreshFiles()">
        <input type="hidden" name="path" value="{{.Path}}">
        <input type="file" name="files" multiple>
        <button class="btn-mini" type="submit">上传</button>
      </form>
    </div>
    {{end}}

    <table class="ftable">
      <thead><tr><th>名称</th><th class="w120">大小</th><th class="w160">修改时间</th><th class="w220">操作</th></tr></thead>
      <tbody>
      {{if and (not .IsSearch) .Parent}}
        <tr><td colspan="4"><a hx-get="/partials/files?path={{urlquery .Parent}}" hx-target="#content">⬆ 返回上级</a></td></tr>
      {{end}}
      {{range .Entries}}
        <tr>
          <td>
            {{if .IsDir}}<span class="fico">📁</span>{{else}}<span class="fico">📄</span>{{end}}
            <a hx-get="{{.Href}}" {{if .IsDir}}hx-target="#content"{{end}}>{{.Name}}</a>
          </td>
          <td>{{.Size}}</td>
          <td>{{.ModTime}}</td>
          <td>
            {{if not .IsDir}}
              <a class="btn-mini" href="{{.Href}}">下载</a>
              <button class="btn-mini" hx-prompt="新名称" hx-post="/api/files/rename"
                      hx-vals='{{.HxVals}}' hx-swap="none"
                      hx-on::after-request="if(event.detail.successful) refreshFiles()">重命名</button>
              <button class="btn-mini" hx-post="/api/files/share" hx-vals='{{.HxVals}}' hx-swap="none"
                      hx-on::after-request="shareDone(event)">分享</button>
            {{end}}
            <button class="btn-mini danger" hx-confirm="确定删除 {{.Name}} ?" hx-post="/api/files/delete"
                    hx-vals='{{.HxVals}}' hx-swap="none"
                    hx-on::after-request="if(event.detail.successful) refreshFiles()">删除</button>
          </td>
        </tr>
      {{else}}
        <tr><td colspan="4" class="fempty">{{if .IsSearch}}无匹配结果{{else}}目录为空{{end}}</td></tr>
      {{end}}
      </tbody>
    </table>
  </div>
  <script>
  window.refreshFiles = function(){
    var el = document.getElementById('files-panel');
    if (!el) return;
    htmx.ajax('GET', '/partials/files?path=' + encodeURIComponent(el.dataset.path), {target: '#content'});
  };
  window.shareDone = function(evt){
    if (!evt.detail.successful) return;
    var full = location.origin + JSON.parse(evt.detail.xhr.responseText).url;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(full).then(function(){ alert('分享链接已复制: ' + full); });
    } else {
      prompt('分享链接:', full);
    }
  };
  </script>
</div>
`))
