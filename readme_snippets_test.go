package doger_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/kartaladev/doger"
)

// The README quickstart, compiled. The text between the readme markers is
// the README block, character for character after dedenting.

// readme:quickstart
type DB struct{ dsn string }

type Server struct{ db *DB }

func (s *Server) Start(ctx context.Context) error {
	fmt.Println("serving with", s.db.dsn)
	return nil
}

func main() {
	root := doger.New()
	doger.ProvideValue(root, &DB{dsn: "postgres://localhost/app"})
	doger.Provide(root, func(i doger.Injector) (*Server, error) {
		db, err := doger.Invoke[*DB](i)
		return &Server{db: db}, err
	}, doger.AsStarter()) // root.Start starts it after the services it invokes

	if err := root.Start(); err != nil {
		log.Fatal(err)
	}
	// shuts services down in reverse dependency order
	if err := root.Shutdown().Err(); err != nil {
		log.Fatal(err)
	}
}

// readme:end

func Example_readmeQuickstart() {
	main()

	// Output:
	// serving with postgres://localhost/app
}

// readmeSnippet matches a README Go block and the name of the snippet
// comment before it.
var readmeSnippet = regexp.MustCompile("(?s)(?:<!-- snippet: ([a-z-]+) -->\n)?```go\n(.*?)```")

// readmeRegion matches a named region of a compiled snippet file.
var readmeRegion = regexp.MustCompile(`(?s)// readme:([a-z-]+)\n(.*?)\n[\t ]*// readme:end`)

// readmeSnippetFiles are the compiled snippet files, by whether a copy of
// the root module without its sibling modules may lack them.
var readmeSnippetFiles = map[string]bool{
	"readme_snippets_test.go":                  false,
	"scheduler/gocron/readme_snippets_test.go": true,
}

// TestReadme_SnippetsCompiled pins that every Go block of README.md is,
// as a whole and in order, the named region of a compiled snippet file:
// a changed, added, deleted or moved line of a block fails it. Only the
// gocron block is skipped, when its sibling module is not in this copy.
func TestReadme_SnippetsCompiled(t *testing.T) {
	t.Parallel()

	readme, err := readLF("README.md")
	if err != nil {
		t.Fatal(err)
	}

	regions := map[string]string{}
	missing := map[string]bool{} // regions of an absent optional file
	for path, optional := range readmeSnippetFiles {
		content, err := readLF(path)
		if optional && errors.Is(err, fs.ErrNotExist) {
			missing["gocron"] = true
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range readmeRegion.FindAllStringSubmatch(content, -1) {
			regions[match[1]] = dedent(match[2])
		}
	}

	blocks := readmeSnippet.FindAllStringSubmatch(readme, -1)
	if len(blocks) == 0 {
		t.Fatal("README.md has no Go block")
	}
	for _, block := range blocks {
		name, text := block[1], dedent(block[2])
		if name == "" {
			t.Errorf("README Go block without a <!-- snippet: name --> comment:\n%s", text)
			continue
		}
		if missing[name] {
			t.Logf("snippet %q: its compiled file is not in this copy of the module", name)
			continue
		}
		region, ok := regions[name]
		if !ok {
			t.Errorf("snippet %q has no // readme:%s region in a compiled file", name, name)
			continue
		}
		if text != region {
			t.Errorf("snippet %q differs from its compiled region\nREADME:\n%s\ncompiled:\n%s", name, text, region)
		}
	}
}

// readLF returns the text of the repository file path with CRLF line
// endings turned into LF, so a Windows checkout with core.autocrlf parses
// as on any other OS.
func readLF(path string) (string, error) {
	content, err := os.ReadFile(path) //nolint:gosec // G304: a fixed path of this repository
	return strings.ReplaceAll(string(content), "\r\n", "\n"), err
}

// dedent removes trailing spaces, surrounding blank lines and the
// indentation common to every non-blank line.
func dedent(text string) string {
	lines := strings.Split(strings.Trim(text, "\n"), "\n")
	common := -1
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		lines[i] = line
		if line == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, "\t"))
		if common < 0 || indent < common {
			common = indent
		}
	}
	for i, line := range lines {
		if len(line) >= common && common > 0 {
			lines[i] = line[common:]
		}
	}

	return strings.Join(lines, "\n")
}
