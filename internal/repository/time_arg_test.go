package repository

import (
	"database/sql/driver"
	"time"
)

type beijingTimeArg struct{}

func (beijingTimeArg) Match(v driver.Value) bool {
	t, ok := v.(time.Time)
	if !ok {
		return false
	}
	_, offset := t.Zone()
	return offset == 8*60*60
}
