package buildfile

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// testdata/instants.json is what Jib's Instants made of each time, with
// testdata/java/Instants.java. Where Java strays from ISO 8601, gib
// refuses instead: a day past its month's end, which Java moves to the
// month's last day; an offset given twice, "Z+0000"; and digits other
// than ASCII ones.
func TestInstantReadsTimesAsJibDoes(t *testing.T) {
	raw, err := os.ReadFile("testdata/instants.json")
	require.NoError(t, err)
	var cases []struct {
		In      string
		Seconds int64
		Nanos   int
		Refused bool
	}
	require.NoError(t, json.Unmarshal(raw, &cases))
	for _, c := range cases {
		got, err := instant(c.In, "creationTime")
		if c.Refused || strays(c.In) {
			require.EqualError(t, err, "creationTime must be a number of milliseconds since epoch or an ISO 8601 formatted date", "%q", c.In)
			continue
		}
		require.NoError(t, err, "%q", c.In)
		require.Equal(t, [2]int64{c.Seconds, int64(c.Nanos)}, [2]int64{got.Unix(), int64(got.Nanosecond())}, "%q", c.In)
	}
}

var (
	pastMonthEnd = regexp.MustCompile(`^[+-]?\d+-(02-(29|3.)|0[469]-31|11-31)[Tt]`)
	twoOffsets   = regexp.MustCompile(`([zZ]|[+-]\d\d:\d\d(:\d\d)?)(\[[^]]*\])?[+-]\d\d(\d\d)?$`)
)

// strays is whether Java reads s where ISO 8601 has no such time.
func strays(s string) bool {
	if m := pastMonthEnd.FindString(s); m != "" {
		y, _ := strconv.ParseInt(strings.SplitN(strings.TrimLeft(s, "+-"), "-", 2)[0], 10, 64)
		if !strings.Contains(m, "-02-29") || !leap(y) {
			return true
		}
	}
	return twoOffsets.MatchString(s) || strings.ContainsFunc(s, func(r rune) bool { return r > 0x7f })
}
