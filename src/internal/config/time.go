package config

import "time"

// nowISO 返回本地时间的 ISO8601 字符串(精确到秒)。
func nowISO() string {
	return time.Now().Format("2006-01-02T15:04:05-07:00")
}
