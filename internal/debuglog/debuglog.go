package debuglog

import (
	"log"
	"os"
)

// Enabled 返回应用层调试日志是否开启。
func Enabled() bool {
	switch os.Getenv("APP_DEBUG") {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

// Printf 在 APP_DEBUG 开启时输出应用层调试日志。
func Printf(format string, args ...interface{}) {
	if Enabled() {
		log.Printf("[app_debug] "+format, args...)
	}
}
