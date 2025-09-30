package postgres

import (
	"math"
	"time"
)

func unixFloatToTime(ts float64) (time.Time, bool) {
	if ts == 0 || math.IsNaN(ts) || math.IsInf(ts, 0) {
		return time.Time{}, false
	}
	sec, frac := math.Modf(ts)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC(), true
}

func timeToUnixFloat(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}

func timePtrOrNil(t time.Time, ok bool) interface{} {
	if !ok || t.IsZero() {
		return nil
	}
	return t
}
