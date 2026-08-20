// Package files 实现文件管理模块(M3):浏览/上传/下载/建目录/重命名/删除/搜索/分享。
//
// 所有用户提供的路径一律经 Normalize 规范化并强制限制在文件根目录内,
// 拒绝绝对路径与 ".." 逃逸。注意:不做符号链接跟随(避免链接逃逸根目录)。
package files

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// drivePrefixRe 匹配 Windows 盘符前缀(如 "C:", "c:\")。
// 注意:filepath.IsAbs 对 "C:foo"(相对盘符路径)返回 false,需单独拦截。
var drivePrefixRe = regexp.MustCompile(`^[A-Za-z]:[\\/]?`)

// 路径相关错误。
var (
	ErrAbsPath   = errors.New("不支持绝对路径")
	ErrOutOfRoot = errors.New("路径越界")
	ErrNotExist  = errors.New("路径不存在")
)

// Normalize 将用户提供的相对路径规范化并限制在 root 内。
// 空路径、"/"、"." 视为 root 本身。返回 root 内的绝对路径。
func Normalize(root, rel string) (string, error) {
	if rel == "" || rel == "/" || rel == "." {
		return root, nil
	}
	// 统一拒绝绝对路径,且不依赖当前平台分隔符语义:
	//  1) filepath.IsAbs: 当前平台的绝对路径(Windows 上会命中 UNC/反斜杠)
	//  2) 前导 "/":POSIX 绝对路径
	//  3) 前导 "\\":UNC 路径(在 Linux/Termux 上 \ 不是分隔符,需显式拦截,
	//     否则 \\server\share 会被当作普通相对路径放行)
	//  4) 盘符前缀(含 "C:foo" 相对盘符变体)
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") ||
		strings.HasPrefix(rel, `\\`) || drivePrefixRe.MatchString(rel) {
		return "", ErrAbsPath
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrOutOfRoot
	}
	full := filepath.Join(root, clean)
	if !isWithin(root, full) {
		return "", ErrOutOfRoot
	}
	return full, nil
}

// isWithin 判断 child 是否严格位于 parent(含自身)之内,做字符串级校验。
func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// SafeName 校验单个文件名(仅允许基础名,禁止分隔符与 ".." 等)。
func SafeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name != filepath.Base(name) {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return true
}

// Rel 返回 root 内的相对路径(供前端使用);不在 root 内返回原样。
func Rel(root, full string) string {
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
