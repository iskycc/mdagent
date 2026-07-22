package timeutil

import "time"

var beijingLocation = loadLocation("Asia/Shanghai")

func loadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err == nil {
		return loc
	}
	return time.FixedZone("Asia/Shanghai", 8*60*60)
}

// BeijingLocation 返回北京时间时区。
func BeijingLocation() *time.Location {
	return beijingLocation
}

// Now 返回当前北京时间。
func Now() time.Time {
	return time.Now().In(beijingLocation)
}

// Date 返回当前北京时间日期。
func Date() string {
	return Now().Format("2006-01-02")
}

// DateTime 返回当前北京时间日期时间。
func DateTime() string {
	return Now().Format("2006-01-02 15:04:05")
}

// FormatMinute 把时间按北京时间格式化到分钟。
func FormatMinute(t time.Time) string {
	return t.In(beijingLocation).Format("2006-01-02 15:04")
}
