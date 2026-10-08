package buildfile

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// instant reads a time as Jib's Instants.fromMillisOrIso8601 does:
// milliseconds since the epoch, or else a date-time as
// DateTimeFormatter.ISO_DATE_TIME parses it, or with an offset of the
// form "+HHmm" where it has none, kept to the nanosecond.
func instant(s, field string) (time.Time, error) {
	if ms, ok := millis(s); ok {
		return time.UnixMilli(ms).UTC(), nil
	}
	if t, err := iso(s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%s must be a number of milliseconds since epoch or an ISO 8601 formatted date", field)
}

// millis is a long as Long.parseLong reads one in ASCII: an optional
// sign, then decimal digits.
func millis(s string) (int64, bool) {
	digits := strings.TrimLeft(s, "+-")
	if len(s)-len(digits) > 1 || digits == "" || strings.ContainsFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// errHard is a parse that fails outright, where Java throws rather
// than trying what follows.
var errHard = errors.New("unparseable")

// iso parses s as Jib's formatter does and resolves it to an instant: a
// date and time, then either an offset with an optional zone in
// brackets or a "+HHmm" offset.
func iso(s string) (time.Time, error) {
	year, i, err := isoYear(s, 0)
	if err != nil {
		return time.Time{}, err
	}
	var month, day, hour, minute, second, nanos int
	steps := []func() bool{
		func() bool { return lit(s, &i, '-') },
		func() bool { return two(s, &i, &month) },
		func() bool { return lit(s, &i, '-') },
		func() bool { return two(s, &i, &day) },
		func() bool {
			if i < len(s) && (s[i] == 'T' || s[i] == 't') {
				i++
				return true
			}
			return false
		},
		func() bool { return two(s, &i, &hour) },
		func() bool { return lit(s, &i, ':') },
		func() bool { return two(s, &i, &minute) },
	}
	for _, step := range steps {
		if !step() {
			return time.Time{}, errHard
		}
	}
	if j := i; lit(s, &j, ':') && two(s, &j, &second) {
		i = j
		if i < len(s) && s[i] == '.' {
			i++
			n := 0
			for n < 9 && i < len(s) && s[i] >= '0' && s[i] <= '9' {
				nanos = nanos*10 + int(s[i]-'0')
				i++
				n++
			}
			for ; n < 9; n++ {
				nanos *= 10
			}
		}
	}

	var offset *int
	// ISO_DATE_TIME's offset, "Z" or "+HH:MM" with optional ":ss", and
	// a zone in brackets after it.
	if off, j, err := offsetID(s, i, true); err == errHard {
		return time.Time{}, err
	} else if err == nil {
		offset, i = &off, j
		if j < len(s) && s[j] == '[' {
			if k, err := region(s, j+1); err == errHard {
				return time.Time{}, err
			} else if err == nil && k < len(s) && s[k] == ']' {
				i = k + 1
			}
		}
	}
	// The "+HHmm" offset Jib appends.
	if offset == nil {
		if off, j, err := offsetHHmm(s, i); err == errHard {
			return time.Time{}, err
		} else if err == nil {
			offset, i = &off, j
		}
	}
	if i != len(s) || offset == nil {
		return time.Time{}, errHard
	}
	return resolve(year, month, day, hour, minute, second, nanos, *offset)
}

// isoYear is the year as appendValue(YEAR, 4, 10, EXCEEDS_PAD) parses
// it strictly: four digits, or more after a sign, and never minus zero.
func isoYear(s string, i int) (int64, int, error) {
	sign := byte(0)
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		sign = s[i]
		i++
	}
	start := i
	var v int64
	for i < len(s) && i-start < 10 && s[i] >= '0' && s[i] <= '9' {
		v = v*10 + int64(s[i]-'0')
		i++
	}
	n := i - start
	switch {
	case n < 4,
		sign == '-' && v == 0,
		sign == '+' && n <= 4,
		sign == 0 && n > 4:
		return 0, 0, errHard
	}
	if sign == '-' {
		v = -v
	}
	return v, i, nil
}

func lit(s string, i *int, c byte) bool {
	if *i < len(s) && s[*i] == c {
		*i++
		return true
	}
	return false
}

func two(s string, i *int, v *int) bool {
	if *i+2 > len(s) || !isDigit(s[*i]) || !isDigit(s[*i+1]) {
		return false
	}
	*v = int(s[*i]-'0')*10 + int(s[*i+1]-'0')
	*i += 2
	return true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// offsetID parses appendOffset("+HH:MM:ss", noOffset): the no-offset
// text, or a sign, hours, colon and minutes, and optional seconds.
func offsetID(s string, i int, z bool) (int, int, error) {
	if i == len(s) {
		return 0, i, errors.New("none")
	}
	if z && (s[i] == 'Z' || s[i] == 'z') {
		return 0, i + 1, nil
	}
	return signed(s, i, true)
}

// offsetHHmm parses appendOffset("+HHmm", "+0000"): a sign, hours and
// optional minutes.
func offsetHHmm(s string, i int) (int, int, error) {
	if i == len(s) {
		return 0, i, errors.New("none")
	}
	return signed(s, i, false)
}

// signed parses OffsetIdPrinterParser's sign and two-digit fields,
// each at most 59: hours, minutes (after a colon and required, when
// colon) and optional seconds, failing outright on hours past 23.
func signed(s string, i int, colon bool) (int, int, error) {
	none := errors.New("none")
	if s[i] != '+' && s[i] != '-' {
		return 0, i, none
	}
	neg := s[i] == '-'
	j := i + 1
	field := func(sep bool) (int, bool) {
		k := j
		if sep {
			if k >= len(s) || s[k] != ':' {
				return 0, false
			}
			k++
		}
		var v int
		if !two(s, &k, &v) || v > 59 {
			return 0, false
		}
		j = k
		return v, true
	}
	h, ok := field(false)
	if !ok {
		return 0, i, none
	}
	m, ok := field(colon)
	if colon && !ok {
		return 0, i, none
	}
	var sec int
	if colon {
		sec, _ = field(true)
	}
	if h > 23 {
		return 0, i, errHard
	}
	v := h*3600 + m*60 + sec
	if neg {
		v = -v
	}
	return v, j, nil
}

// region parses appendZoneRegionId case sensitively, as
// ZoneIdPrinterParser does: an offset, UTC, UT or GMT with an optional
// offset, GMT0, a region id of Java's, or Z; it gives where it ends.
func region(s string, i int) (int, error) {
	none := errors.New("none")
	if i == len(s) {
		return 0, none
	}
	if c := s[i]; c == '+' || c == '-' {
		off, j, err := offsetID(s, i, false)
		if err != nil {
			return 0, err
		}
		if !offsetInRange(off) {
			return 0, none
		}
		return j, nil
	}
	if len(s) >= i+2 {
		switch {
		case s[i] == 'U' && s[i+1] == 'T':
			if len(s) >= i+3 && s[i+2] == 'C' {
				if len(s) == i+3 || s[i+3] == '+' || s[i+3] == '-' {
					return offsetBased(s, i+3)
				}
			} else {
				return offsetBased(s, i+2)
			}
		case s[i] == 'G' && len(s) >= i+3 && s[i+1] == 'M' && s[i+2] == 'T':
			if len(s) >= i+4 && s[i+3] == '0' {
				return i + 4, nil
			}
			return offsetBased(s, i+3)
		}
	}
	best := ""
	for id := range zoneIDs {
		if len(id) > len(best) && strings.HasPrefix(s[i:], id) {
			best = id
		}
	}
	switch {
	case best != "":
		return i + len(best), nil
	case s[i] == 'Z':
		return i + 1, nil
	}
	return 0, none
}

// offsetBased is what follows UTC, UT or GMT: an offset, or nothing.
func offsetBased(s string, i int) (int, error) {
	if i >= len(s) || s[i] == '0' || s[i] == 'Z' {
		return i, nil
	}
	off, j, err := offsetID(s, i, false)
	switch {
	case err == errHard:
		return 0, err
	case err != nil:
		return i, nil
	case !offsetInRange(off):
		return 0, errors.New("none")
	}
	return j, nil
}

func offsetInRange(secs int) bool { return secs >= -18*3600 && secs <= 18*3600 }

const maxYear = 999_999_999

// resolve is the instant of a parsed date-time, every field within its
// range and the day within its month, 24:00 being the next day's
// start.
func resolve(year int64, month, day, hour, minute, second, nanos, offset int) (time.Time, error) {
	switch {
	case year < -maxYear || year > maxYear,
		month < 1 || month > 12,
		day < 1 || day > 31,
		minute > 59,
		!offsetInRange(offset):
		return time.Time{}, errHard
	}
	extra := int64(0)
	if hour == 24 && minute == 0 && second == 0 && nanos == 0 {
		hour, extra = 0, 1
	} else if hour > 23 || second > 59 {
		return time.Time{}, errHard
	}
	switch month {
	case 4, 6, 9, 11:
		if day > 30 {
			return time.Time{}, errHard
		}
	case 2:
		if day > 29 || day == 29 && !leap(year) {
			return time.Time{}, errHard
		}
	}
	days := epochDay(year, month, day) + extra
	if extra == 1 && month == 12 && day == 31 && year == maxYear {
		return time.Time{}, errHard
	}
	secs := days*86400 + int64(hour*3600+minute*60+second) - int64(offset)
	return time.Unix(secs, int64(nanos)).UTC(), nil
}

// epochDay is LocalDate.toEpochDay.
func epochDay(year int64, month, day int) int64 {
	y, m := year, int64(month)
	total := 365 * y
	if y >= 0 {
		total += (y+3)/4 - (y+99)/100 + (y+399)/400
	} else {
		total -= y/-4 - y/-100 + y/-400
	}
	total += (367*m - 362) / 12
	total += int64(day) - 1
	if m > 2 {
		total--
		if !leap(y) {
			total--
		}
	}
	return total - 719528
}

func leap(y int64) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }
