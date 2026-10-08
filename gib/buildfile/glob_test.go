package buildfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each case is what Java's FileSystems.getDefault().getPathMatcher
// ("glob:" + pattern).matches(Paths.get(path)) answers, run under
// JDK 17, the trailing slash expanded as Jib expands it.
func TestPathMatcherMatchesAsJavaDoes(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"**/*.txt", "./dir/a.txt", true},
		{"**/*.txt", "a.txt", false},
		{"*.txt", "./dir/a.txt", false},
		{"*.txt", "a.txt", true},
		{"./dir/*.txt", "./dir/a.txt", true},
		{"./dir/*.txt", "./dir/sub/b.txt", false},
		{"**/sub/**", "./dir/sub/b.txt", true},
		{"**/sub/**", "./dir/sub", false},
		{"**/sub", "./dir/sub", true},
		{"**/sub/", "./dir/sub/b.txt", true},
		{"?.txt", "a.txt", true},
		{"?.txt", "/.txt", false},
		{"**/[ab].txt", "./dir/a.txt", true},
		{"**/[!ab].txt", "./dir/c.txt", true},
		{"**/[!ab].txt", "./dir/a.txt", false},
		{"**/[a-c].txt", "./dir/b.txt", true},
		{"**/{a,b}.txt", "./dir/b.txt", true},
		{"**/{a,b}.txt", "./dir/c.txt", false},
		{"**/a.{txt,md}", "./x/a.md", true},
		{`**/\*.txt`, "./dir/*.txt", true},
		{"**/a.(x)", "./d/a.(x)", true},
		{"**/a+b", "./d/a+b", true},
		{"**/[+-0]", "./d/.", true},
		{"**/[+-0]", "./d/0", true},
		{"**", "./dir", true},
		{"**/*", "./dir", true},
		{"dir/**", "dir/a/b", true},
		{"**/.hidden", "./d/.hidden", true},
		{"**/[-a]", "./d/-", true},
		{"**/[a-]", "./d/-", true},
	}
	for _, c := range cases {
		m, err := newPathMatcher(c.glob)
		require.NoError(t, err, c.glob)
		assert.Equal(t, c.want, m.matches(c.path), "%s against %s", c.glob, c.path)
	}
}

func TestPathMatcherRefusesWhatJavaRefuses(t *testing.T) {
	for _, glob := range []string{"{a,{b}}", "{a", "[a", "[/]", "[b-a]"} {
		_, err := newPathMatcher(glob)
		assert.Error(t, err, glob)
	}
}
