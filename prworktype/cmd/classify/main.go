// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Command classify is the Go batch runner: a CSV of PRs through the cascade
// (rules + routing in Go, embedding/LLM via the Python model server) to a results
// CSV. Mirrors the Python classify_csv.py; the model layers stay in Python.
//
//	go run ./cmd/classify --in prs.csv --out results.csv --model-url http://localhost:8000 --llm
//
// Input CSV needs a `title` column; branch/author_login/files/body are optional
// (files pipe-separated: "a.go|b.go").
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/prworktype"
)

func main() {
	in := flag.String("in", "", "input CSV (required)")
	out := flag.String("out", "", "output CSV (required)")
	modelURL := flag.String("model-url", "http://localhost:8000", "Python model server base URL")
	useLLM := flag.Bool("llm", false, "enable the LLM layer (model server must have GEMINI_API_KEY)")
	topk := flag.Int("topk", 0, "0 = full taxonomy on escalation")
	dbDSN := flag.String("db", "", "Postgres DSN to also persist results (optional)")
	flag.Parse()
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "both --in and --out are required")
		os.Exit(2)
	}

	rows, header, err := readCSV(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	col := index(header)
	if _, ok := col["title"]; !ok {
		fmt.Fprintln(os.Stderr, "input CSV must have a 'title' column")
		os.Exit(1)
	}

	clf := prworktype.NewClassifier(prworktype.NewModelClient(*modelURL), *useLLM)
	clf.TopK = *topk
	ctx := context.Background()

	var store *prworktype.Storage
	if *dbDSN != "" {
		store, err = prworktype.OpenStorage(*dbDSN)
		if err != nil {
			fmt.Fprintln(os.Stderr, "db:", err)
			os.Exit(1)
		}
		defer func() { _ = store.Close() }()
	}

	mode := "embedding-only"
	if *useLLM {
		mode = "full cascade"
	}
	fmt.Printf("classifying %d PRs (%s) …\n", len(rows), mode)

	results := [][]string{{"title", "branch", "type_bucket", "type_method", "type_score", "domain_bucket", "domain_method"}}
	for _, r := range rows {
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(r) {
				return r[i]
			}
			return ""
		}
		var files []string
		if f := get("files"); f != "" {
			files = strings.Split(f, "|")
		}
		c, err := clf.Classify(ctx, prworktype.Input{
			Title: get("title"), Branch: get("branch"), AuthorLogin: get("author_login"),
			Files: files, Body: get("body"),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "classify:", err)
			os.Exit(1)
		}
		score := ""
		if c.Type.Method == prworktype.MethodEmbedding {
			score = strconv.FormatFloat(c.Type.Score, 'f', 4, 64)
		}
		results = append(results, []string{
			get("title"), get("branch"), c.Type.Bucket, string(c.Type.Method), score,
			c.Domain.Bucket, string(c.Domain.Method),
		})
		if store != nil {
			if err := store.Save(get("title"), get("branch"), c); err != nil {
				fmt.Fprintln(os.Stderr, "db save:", err)
				os.Exit(1)
			}
		}
	}

	if err := writeCSV(*out, results); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d rows -> %s\n", len(results)-1, *out)
}

func readCSV(path string) ([][]string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	rec, err := csv.NewReader(f).ReadAll()
	if err != nil || len(rec) == 0 {
		return nil, nil, err
	}
	return rec[1:], rec[0], nil
}

func writeCSV(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	defer w.Flush()
	return w.WriteAll(rows)
}

func index(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.TrimSpace(h)] = i
	}
	return m
}
