package files

import (
	"database/sql"
	"errors"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// maxSearchResults 搜索返回上限,防止大目录树拖垮请求。
const maxSearchResults = 200

// maxSearchDepth 搜索递归深度上限。
const maxSearchDepth = 8

// Store 文件管理(作用于配置的文件根目录)。
type Store struct {
	root string
	db   *sql.DB
	log  *slog.Logger
}

// NewStore 创建文件管理存储。root 为文件根目录(须已存在)。
func NewStore(root string, db *sql.DB, log *slog.Logger) *Store {
	return &Store{root: root, db: db, log: log}
}

// Root 返回文件根目录。
func (s *Store) Root() string { return s.root }

// Entry 目录条目。
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"` // 相对路径(供后续请求使用)
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

// list 读取目录,返回排序后的条目。
func (s *Store) list(rel string) ([]Entry, error) {
	full, err := Normalize(s.root, rel)
	if err != nil {
		return nil, err
	}
	dir, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotExist
		}
		return nil, err
	}
	defer dir.Close()
	infos, err := dir.Readdir(-1)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(infos))
	for _, info := range infos {
		entry := Entry{
			Name:    info.Name(),
			Path:    Rel(s.root, filepath.Join(full, info.Name())),
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04"),
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			continue // 跳过特殊文件(socket/fifo 等)
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// HandleList GET /api/files/list?path= → {path, entries}。
func (s *Store) HandleList(c *fiber.Ctx) error {
	entries, err := s.list(c.Query("path", ""))
	if err != nil {
		return s.respondErr(c, err)
	}
	return c.JSON(fiber.Map{
		"path":    Rel(s.root, s.root) + relQuery(c.Query("path", "")),
		"entries": entries,
	})
}

// relQuery 返回规范化后的相对路径(用于响应回显)。
func relQuery(rel string) string {
	if rel == "" || rel == "/" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	return "/" + strings.TrimPrefix(clean, "/")
}

// HandleMkdir POST /api/files/mkdir {path, name}(name 亦可经 HX-Prompt 头传递)。
func (s *Store) HandleMkdir(c *fiber.Ctx) error {
	var p struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := c.BodyParser(&p); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "请求格式无效"})
	}
	if p.Name == "" {
		p.Name = c.Get("HX-Prompt")
	}
	if !SafeName(p.Name) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "目录名无效"})
	}
	full, err := Normalize(s.root, joinRel(p.Path, p.Name))
	if err != nil {
		return s.respondErr(c, err)
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		s.log.Error("建目录失败", "path", full, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "创建目录失败"})
	}
	return c.JSON(fiber.Map{"ok": true, "path": Rel(s.root, full)})
}

// HandleUpload POST /api/files/upload(multipart:path 字段 + files)。
func (s *Store) HandleUpload(c *fiber.Ctx) error {
	dirRel := c.FormValue("path", "")
	full, err := Normalize(s.root, dirRel)
	if err != nil {
		return s.respondErr(c, err)
	}
	form, err := c.MultipartForm()
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "请求不是 multipart 表单"})
	}
	files := form.File["files"]
	if len(files) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "未选择文件"})
	}
	uploaded := 0
	for _, fh := range files {
		if !SafeName(fh.Filename) {
			continue
		}
		// M5 安全加固:单文件上限 256 MiB(手机内存有限,避免整体缓冲)
		if fh.Size > maxUploadSize {
			s.log.Warn("拒绝超大上传", "name", fh.Filename, "size", fh.Size)
			continue
		}
		dst := filepath.Join(full, fh.Filename)
		if err := c.SaveFile(fh, dst); err != nil {
			s.log.Error("保存上传文件失败", "name", fh.Filename, "err", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "保存文件失败: " + fh.Filename})
		}
		uploaded++
	}
	return c.JSON(fiber.Map{"ok": true, "uploaded": uploaded})
}

// maxUploadSize 单文件上传上限(256 MiB;手机内存有限,避免超大文件整体缓冲)。
const maxUploadSize = 256 << 20

// HandleDownload GET /api/files/download?path= → 流式下载。
func (s *Store) HandleDownload(c *fiber.Ctx) error {
	full, err := Normalize(s.root, c.Query("path", ""))
	if err != nil {
		return s.respondErr(c, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return s.respondErr(c, err)
	}
	if info.IsDir() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "不能下载目录"})
	}
	return s.serveFile(c, full, info.Name(), false)
}

// serveFile 流式输出文件。
// 不使用 Fiber SendFile(其经 fasthttp.FS 缓存句柄 ~10s,Windows 下会锁住文件无法删除)。
// 手动打开-流式输出;文件句柄所有权交给 fasthttp,响应写完后由其关闭。
func (s *Store) serveFile(c *fiber.Ctx, full, name string, inline bool) error {
	f, err := os.Open(full)
	if err != nil {
		return s.respondErr(c, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return s.respondErr(c, err)
	}
	ctype := mime.TypeByExtension(filepath.Ext(full))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	// 安全防线:可执行内容类型禁止内联渲染(防存储型 XSS),
	// 一律强制下载,文件根内的 html/svg/xml/js 无法在同源域执行。
	if inline && isInlineUnsafe(ctype) {
		inline = false
	}
	c.Set("Content-Type", ctype)
	c.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if !inline {
		c.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	c.Context().SetBodyStream(f, int(info.Size()))
	return nil
}

// isInlineUnsafe 判断 MIME 类型是否可在浏览器内联执行脚本(存储型 XSS 风险)。
func isInlineUnsafe(ctype string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(ctype, ";", 2)[0]))
	switch ct {
	case "text/html", "application/xhtml+xml", "image/svg+xml",
		"text/xml", "application/xml", "text/javascript", "application/javascript",
		"application/json":
		return true
	}
	return false
}

// HandleRename POST /api/files/rename {path, new_name}(new_name 亦可经 HX-Prompt 头传递)。
func (s *Store) HandleRename(c *fiber.Ctx) error {
	var p struct {
		Path    string `json:"path"`
		NewName string `json:"new_name"`
	}
	if err := c.BodyParser(&p); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "请求格式无效"})
	}
	if p.NewName == "" {
		p.NewName = c.Get("HX-Prompt")
	}
	if !SafeName(p.NewName) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "新名称无效"})
	}
	oldFull, err := Normalize(s.root, p.Path)
	if err != nil {
		return s.respondErr(c, err)
	}
	newFull := filepath.Join(filepath.Dir(oldFull), p.NewName)
	if !isWithin(s.root, newFull) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "路径越界"})
	}
	if _, err := os.Stat(oldFull); err != nil {
		return s.respondErr(c, err)
	}
	if err := os.Rename(oldFull, newFull); err != nil {
		s.log.Error("重命名失败", "from", oldFull, "to", newFull, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "重命名失败"})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// HandleDelete POST /api/files/delete {path}(目录递归删除)。
func (s *Store) HandleDelete(c *fiber.Ctx) error {
	var p struct {
		Path string `json:"path"`
	}
	if err := c.BodyParser(&p); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "请求格式无效"})
	}
	full, err := Normalize(s.root, p.Path)
	if err != nil {
		return s.respondErr(c, err)
	}
	if full == s.root {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "不能删除根目录"})
	}
	// Windows 下刚被下载/读取的文件句柄可能未及时释放,短时重试。
	const attempts = 5
	for i := 0; i < attempts; i++ {
		err = os.RemoveAll(full)
		if err == nil {
			return c.JSON(fiber.Map{"ok": true})
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.log.Error("删除失败", "path", full, "err", err)
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "删除失败"})
}

// HandleSearch GET /api/files/search?q= → 递归搜索(名称子串,不区分大小写)。
func (s *Store) HandleSearch(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q", ""))
	if q == "" {
		return c.JSON(fiber.Map{"results": []Entry{}})
	}
	results, err := s.searchAll(q)
	if err != nil {
		s.log.Error("搜索失败", "q", q, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "搜索失败"})
	}
	return c.JSON(fiber.Map{"results": results})
}

// searchAll 在文件根目录下递归搜索名称子串(不区分大小写)。
func (s *Store) searchAll(q string) ([]Entry, error) {
	q = strings.ToLower(q)
	results := make([]Entry, 0, 64)
	_ = filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == s.root {
			return nil
		}
		if len(results) >= maxSearchResults {
			return filepath.SkipAll
		}
		depth := strings.Count(filepath.ToSlash(path), "/") - strings.Count(filepath.ToSlash(s.root), "/")
		if depth > maxSearchDepth {
			return nil
		}
		if strings.Contains(strings.ToLower(d.Name()), q) {
			if !d.IsDir() && !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			results = append(results, Entry{
				Name:    d.Name(),
				Path:    Rel(s.root, path),
				IsDir:   d.IsDir(),
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02 15:04"),
			})
		}
		return nil
	})
	return results, nil
}

// joinRel 拼接父目录与名称(均在 root 内由 Normalize 兜底)。
func joinRel(parent, name string) string {
	if parent == "" || parent == "/" {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(filepath.Join(parent, name))
}

// respondErr 将内部错误映射为 HTTP 响应。
func (s *Store) respondErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrAbsPath):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "不支持绝对路径"})
	case errors.Is(err, ErrOutOfRoot):
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "路径越界"})
	case errors.Is(err, ErrNotExist), os.IsNotExist(err):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "路径不存在"})
	default:
		s.log.Error("文件操作失败", "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "文件操作失败"})
	}
}

// helper 供 share.go 读取相对路径。
func (s *Store) norm(rel string) (string, error) { return Normalize(s.root, rel) }
