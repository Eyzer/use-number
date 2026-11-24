package main

import "time"

type timeT = time.Time

func timeNow() timeT { return time.Now() }

func timeDate(y int, m time.Month, d int) timeT {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// 取所在周的周一零点
func mondayOf(t timeT) timeT {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return timeDate(t.Year(), t.Month(), t.Day()).AddDate(0, 0, -(wd - 1))
}
