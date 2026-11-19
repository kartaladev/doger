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

// The README quickstart, compiled. Its func main is readmeQuickstart here.

type DB struct{ dsn string }

type Server struct{ db *DB }

func (s *Server) Start(ctx context.Context) error {
	fmt.Println("serving with", s.db.dsn)
	return nil
}

func readmeQuickstart() {
	root := doger.New()
	doger.ProvideValue(root, &DB{dsn: "postgres://localhost/app"})
	doger.Provide(root, func(i doger.Injector) (*Server, error) {
		db, err := doger.Invoke[*DB](i)
		return &Server{db: db}, err
	}, doger.AsStarter()) // started by root.Start, after what it invokes

	if err := root.StartWithContext(context.Background()); err != nil {
		log.Fatal(err)
	}
	_ = root.Shutdown() // shuts services down in reverse dependency order
}

func Example_readmeQuickstart() {
	readmeQuickstart()

	// Output:
	// serving with postgres://localhost/app
}

// readmeGoBlock matches a fenced Go block of README.md.
var readmeGoBlock = regexp.MustCompile("(?s)```go\n(.*?)```")

// TestReadme_SnippetsCompiled pins that every line of every Go snippet of
// README.md appears in a compiled snippet file, so the README cannot drift
// from code that builds: the quickstart here, the gocron snippet in
// scheduler/gocron/readme_snippets_test.go.
func TestReadme_SnippetsCompiled(t *testing.T) {
	t.Parallel()

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	compiled := ""
	for _, path := range []string{"readme_snippets_test.go", "scheduler/gocron/readme_snippets_test.go"} {
		content, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			// a copy of the root module without its sibling modules
			t.Skipf("%s is not in this copy of the module", path)
		}
		if err != nil {
			t.Fatal(err)
		}
		compiled += string(content)
	}
	compiledLines := map[string]bool{}
	for line := range strings.SplitSeq(compiled, "\n") {
		compiledLines[strings.TrimSpace(line)] = true
	}

	blocks := readmeGoBlock.FindAllStringSubmatch(string(readme), -1)
	if len(blocks) == 0 {
		t.Fatal("README.md has no Go snippet")
	}
	for _, block := range blocks {
		for line := range strings.SplitSeq(block[1], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "func main() {" {
				continue
			}
			if !compiledLines[line] {
				t.Errorf("README line not in a compiled snippet: %q", line)
			}
		}
	}
}
