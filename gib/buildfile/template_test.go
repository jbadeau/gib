package buildfile

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// testdata/substitute.json is what Jib's reader made of each text, with
// testdata/java/Substitute.java.
func TestSubstituteReplacesParametersAsJibDoes(t *testing.T) {
	raw, err := os.ReadFile("testdata/substitute.json")
	require.NoError(t, err)
	var golden struct {
		Params map[string]string
		Cases  []struct{ In, Out, Err string }
	}
	require.NoError(t, json.Unmarshal(raw, &golden))
	for _, c := range golden.Cases {
		got, err := substitute(c.In, golden.Params)
		if c.Err != "" {
			require.EqualError(t, err, c.Err, "%q", c.In)
			continue
		}
		require.NoError(t, err, "%q", c.In)
		require.Equal(t, c.Out, got, "%q", c.In)
	}
}
