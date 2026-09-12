package timezone

import (
	"strings"
	"time"
)

// East8 东八区固定时区 (UTC+8 / 北京时间 / CST)
// 使用 FixedZone 确保不受宿主机操作系统是否安装 tzdata 或系统本地时区影响
var East8 = time.FixedZone("CST", 8*3600)

// TagPrecision 航次标签时间精度
type TagPrecision string

const (
	PrecisionDay    TagPrecision = "day"    // 精确到天: 20060102
	PrecisionHour   TagPrecision = "hour"   // 精确到时: 20060102-15
	PrecisionMinute TagPrecision = "minute" // 精确到分: 20060102-1504
	PrecisionSecond TagPrecision = "second" // 精确到秒: 20060102-150405 (默认)
)

// Now 返回当前的东八区标准时间
func Now() time.Time {
	return time.Now().In(East8)
}

// Format 将任意 time.Time 转换为东八区并按指定 layout 格式化
func Format(t time.Time, layout string) string {
	return t.In(East8).Format(layout)
}

// FormatDefault 将任意 time.Time 转换为东八区标准日期时间字符串 (2006-01-02 15:04:05)
func FormatDefault(t time.Time) string {
	return Format(t, "2006-01-02 15:04:05")
}

// GenerateTagTime 默认生成精确到秒的东八区航次标签时间后缀 (20060102-150405)
func GenerateTagTime() string {
	return GenerateTagTimeByPrecision(string(PrecisionSecond))
}

// GenerateTagTimeByPrecision 根据指定精度或格式生成东八区时间后缀
// 支持: day / hour / minute / second，以及自定义 layout (如 20060102 或 2006-01-02)
func GenerateTagTimeByPrecision(precision string) string {
	p := strings.ToLower(strings.TrimSpace(precision))
	switch p {
	case "day", "d", "date", "天", "日":
		return Now().Format("20060102")
	case "hour", "h", "时", "小时":
		return Now().Format("20060102-15")
	case "minute", "min", "m", "分", "分钟":
		return Now().Format("20060102-1504")
	case "second", "sec", "s", "秒", "":
		return Now().Format("20060102-150405")
	default:
		// 支持直接指定自定义时间格式 (只要包含标准年份 2006 或月份 01)
		if strings.Contains(precision, "2006") || strings.Contains(precision, "01") {
			return Now().Format(precision)
		}
		return Now().Format("20060102-150405")
	}
}
