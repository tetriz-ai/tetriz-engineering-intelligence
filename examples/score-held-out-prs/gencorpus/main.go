// Command gencorpus writes the synthetic pull-request corpus that the
// score-held-out-prs example reads. Every diff is made up from a fixed seed:
// no real repository, person or code is in it.
//
//	go run ./examples/score-held-out-prs/gencorpus          # rewrite testdata/corpus.json
//	go run ./examples/score-held-out-prs/gencorpus -check   # fail if it is out of date
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

const corpusPath = "examples/score-held-out-prs/testdata/corpus.json"

// PR is one pull request in the corpus.
type PR struct {
	ID string `json:"id"`
	// Role is "baseline" (the window the baseline is built from) or
	// "held-out" (scored against it).
	Role string `json:"role"`
	// DiffRef is a made-up diff reference, or a sentinel: TOO_LARGE or
	// NOT_AVAILABLE, with no diff.
	DiffRef string `json:"diff_ref"`
	// Format is "unified" or "json" (GitLab), empty with a sentinel.
	Format string `json:"format,omitempty"`
	Diff   string `json:"diff,omitempty"`
}

// fileSpec is one file a synthetic PR adds lines to.
type fileSpec struct {
	path  string
	lines []string
}

func main() {
	check := flag.Bool("check", false, "fail if the corpus on disk differs from a fresh run")
	flag.Parse()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(corpus()); err != nil {
		log.Fatal(err)
	}

	if *check {
		onDisk, err := os.ReadFile(corpusPath)
		if err != nil {
			log.Fatal(err)
		}
		if !bytes.Equal(onDisk, buf.Bytes()) {
			log.Fatalf("%s is out of date: run go run ./examples/score-held-out-prs/gencorpus", corpusPath)
		}
		fmt.Println("corpus up to date")
		return
	}
	if err := os.MkdirAll(filepath.Dir(corpusPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(corpusPath, buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}

// corpus builds 60 baseline PRs and 9 held-out PRs.
func corpus() []PR {
	r := rand.New(rand.NewPCG(2026, 10))
	var prs []PR
	for i := 1; i <= 60; i++ {
		id := fmt.Sprintf("pr-%03d", i)
		switch i {
		case 17:
			prs = append(prs, PR{ID: id, Role: "baseline", DiffRef: "TOO_LARGE"})
			continue
		case 43:
			prs = append(prs, PR{ID: id, Role: "baseline", DiffRef: "NOT_AVAILABLE"})
			continue
		}
		var files []fileSpec
		if chance(r, 0.85) {
			files = append(files, goFiles(r, id, sizeOf(r, 80, 1.0), filesOf(r))...)
		}
		if chance(r, 0.70) {
			files = append(files, tsFiles(r, id, sizeOf(r, 50, 0.9), filesOf(r))...)
		}
		if chance(r, 0.15) {
			files = append(files, pyFiles(id, sizeOf(r, 40, 0.8)))
		}
		if len(files) == 0 {
			files = append(files, goFiles(r, id, sizeOf(r, 80, 1.0), 1)...)
		}
		if chance(r, 0.3) {
			files = append(files, docFile(id, 5+r.IntN(30)))
		}
		if chance(r, 0.2) {
			files = append(files, configFile(id, 3+r.IntN(20)))
		}
		if chance(r, 0.1) {
			files = append(files, generatedFile(id, 50+r.IntN(400)))
		}
		prs = append(prs, PR{ID: id, Role: "baseline", DiffRef: id + ".patch", Format: "unified", Diff: unified(files)})
	}

	held := []struct {
		id    string
		files []fileSpec
		json  bool
	}{
		{"held-small-go", goFiles(r, "h1", 12, 1), false},
		{"held-large-go", goFiles(r, "h2", 600, 9), false},
		{"held-go-and-ts", append(goFiles(r, "h3", 120, 3), tsFiles(r, "h3", 80, 2)...), false},
		{"held-typical-ts", tsFiles(r, "h4", 50, 2), false},
		{"held-python-only", []fileSpec{pyFiles("h5", 60)}, false},
		{"held-docs-and-config", []fileSpec{docFile("h6", 40), configFile("h6", 25)}, false},
		{"held-gitlab-go-vendored", append(goFiles(r, "h9", 90, 2), vendoredFile("h9", 300), lockFile("h9", 120)), true},
	}
	for _, h := range held {
		pr := PR{ID: h.id, Role: "held-out", DiffRef: h.id + ".patch", Format: "unified", Diff: unified(h.files)}
		if h.json {
			pr.DiffRef, pr.Format, pr.Diff = h.id+".json", "json", gitLabJSON(h.files)
		}
		prs = append(prs, pr)
	}
	prs = append(prs,
		PR{ID: "held-too-large", Role: "held-out", DiffRef: "TOO_LARGE"},
		PR{ID: "held-not-available", Role: "held-out", DiffRef: "NOT_AVAILABLE"},
	)
	return prs
}

// chance reports true with probability p.
func chance(r *rand.Rand, p float64) bool { return unit(r) < p }

// unit returns a float in [0, 1) from the generator's 64-bit output, so the
// corpus depends only on PCG's specified sequence.
func unit(r *rand.Rand) float64 { return float64(r.Uint64()>>11) / (1 << 53) }

// sizeOf draws a log-normal line count with the given median and log sigma
// (Box-Muller from two uniform draws).
func sizeOf(r *rand.Rand, median, sigma float64) int {
	u1, u2 := unit(r), unit(r)
	z := math.Sqrt(-2*math.Log(1-u1)) * math.Cos(2*math.Pi*u2)
	n := int(math.Round(median * math.Exp(sigma*z)))
	return max(n, 1)
}

// filesOf draws how many files of one language a PR touches: 1 to 6.
func filesOf(r *rand.Rand) int { return 1 + min(r.IntN(3)+r.IntN(4), 5) }

// split spreads n lines over k files, at least one line each.
func split(n, k int) []int {
	k = max(min(k, n), 1)
	out := make([]int, k)
	for i := range out {
		out[i] = n / k
		if i < n%k {
			out[i]++
		}
	}
	return out
}

func goFiles(r *rand.Rand, id string, sloc, nfiles int) []fileSpec {
	var files []fileSpec
	for f, n := range split(sloc, nfiles) {
		lines := []string{"package widgets", ""}
		for i := 0; i < n-1; i++ {
			if i%5 == 0 {
				lines = append(lines, fmt.Sprintf("// value%d returns a fixed number.", i))
			}
			lines = append(lines, fmt.Sprintf("func value%d() int { return %d }", i, r.IntN(1000)))
		}
		files = append(files, fileSpec{fmt.Sprintf("widgets/%s_%d.go", strings.ReplaceAll(id, "-", "_"), f), lines})
	}
	return files
}

func tsFiles(r *rand.Rand, id string, sloc, nfiles int) []fileSpec {
	var files []fileSpec
	for f, n := range split(sloc, nfiles) {
		var lines []string
		for i := 0; i < n; i++ {
			if i%4 == 0 {
				lines = append(lines, "")
			}
			lines = append(lines, fmt.Sprintf("export const item%d = %d;", i, r.IntN(1000)))
		}
		files = append(files, fileSpec{fmt.Sprintf("web/src/%s-%d.ts", id, f), lines})
	}
	return files
}

func pyFiles(id string, sloc int) fileSpec {
	var lines []string
	for i := 0; i < sloc; i += 2 {
		lines = append(lines, fmt.Sprintf("def task_%d():", i), fmt.Sprintf("    return %d", i))
	}
	return fileSpec{fmt.Sprintf("jobs/%s.py", strings.ReplaceAll(id, "-", "_")), lines}
}

func docFile(id string, n int) fileSpec {
	lines := []string{"# Notes for " + id, ""}
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("Step %d of the made-up procedure.", i+1))
	}
	return fileSpec{"docs/" + id + ".md", lines}
}

func configFile(id string, n int) fileSpec {
	lines := []string{"{"}
	for i := 0; i < n; i++ {
		sep := ","
		if i == n-1 {
			sep = ""
		}
		lines = append(lines, fmt.Sprintf(`  "setting_%d": %d%s`, i, i, sep))
	}
	lines = append(lines, "}")
	return fileSpec{"config/" + id + ".json", lines}
}

func generatedFile(id string, n int) fileSpec {
	lines := []string{"// Code generated by protoc-gen-go. DO NOT EDIT.", "", "package widgetspb"}
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("var field%d = %d", i, i))
	}
	return fileSpec{"widgetspb/" + strings.ReplaceAll(id, "-", "_") + ".pb.go", lines}
}

func vendoredFile(id string, n int) fileSpec {
	lines := []string{"package lib"}
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("var v%d = %d", i, i))
	}
	return fileSpec{"vendor/example.com/lib/" + id + ".go", lines}
}

func lockFile(id string, n int) fileSpec {
	var lines []string
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("package-%s-%d@1.0.%d", id, i, i))
	}
	return fileSpec{"web/deps.lock", lines}
}

// unified renders new files as a git unified diff.
func unified(files []fileSpec) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n", f.path, f.path, f.path)
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(f.lines))
		for _, l := range f.lines {
			b.WriteString("+" + l + "\n")
		}
	}
	return b.String()
}

// gitLabJSON renders new files as GitLab's merge-request diffs JSON.
func gitLabJSON(files []fileSpec) string {
	type entry struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
		NewFile bool   `json:"new_file"`
		Diff    string `json:"diff"`
	}
	var entries []entry
	for _, f := range files {
		var b strings.Builder
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(f.lines))
		for _, l := range f.lines {
			b.WriteString("+" + l + "\n")
		}
		entries = append(entries, entry{f.path, f.path, true, b.String()})
	}
	out, err := json.Marshal(entries)
	if err != nil {
		log.Fatal(err)
	}
	return string(out)
}
