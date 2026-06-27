// Package treesitter computes cyclomatic and cognitive complexity for the
// non-Go languages that go-codemetrics supports, using the pure-Go tree-sitter
// runtime github.com/odvcencio/gotreesitter and its bundled grammars.
//
// It is a separate package from the module root so the root
// (github.com/richardwooding/go-codemetrics) stays dependency-free: programs
// that only analyse Go never compile gotreesitter or embed any grammars.
// Import this package only when you need the other languages.
//
// Grammars are embedded at build time. A plain build embeds every bundled
// grammar (~22 MB); to embed only the languages you use, build with the
// gotreesitter subset tags, e.g.
//
//	-tags 'grammar_subset grammar_subset_python grammar_subset_rust'
//
// Cognitive complexity follows the SonarSource specification and matches the
// Go analyzer in the parent package. It is computed for every supported
// language except Swift (whose grammar lacks a stable cognitive spec); for
// Swift, FunctionMetrics.Cognitive is nil while Cyclomatic is still reported.
package treesitter

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	codemetrics "github.com/richardwooding/go-codemetrics"
)

// parseTimeoutMicros caps a single tree-sitter parse. A pathological grammar
// parse (notably Swift) can run for minutes on a small file and isn't
// cancellable; this bounds it so the offending source yields no metrics rather
// than hanging. 5 s is far above any healthy parse (milliseconds).
const parseTimeoutMicros = 5_000_000

// detectFile maps each canonical language identifier to a representative
// filename so grammars.DetectLanguage resolves the right grammar.
var detectFile = map[string]string{
	"rust":       "x.rs",
	"typescript": "x.ts",
	"javascript": "x.js",
	"ruby":       "x.rb",
	"swift":      "x.swift",
	"kotlin":     "x.kt",
	"c":          "x.c",
	"cpp":        "x.cpp",
	"python":     "x.py",
	"java":       "x.java",
	"csharp":     "x.cs",
	"php":        "x.php",
	"perl":       "x.pl",
	"r":          "x.r",
	"matlab":     "x.m",
	"scala":      "x.scala",
}

// funcSpanQuery supplements the grammar's bundled tags query for languages
// whose tags query doesn't expose a function span (so cyclomatic complexity
// couldn't otherwise be attributed to a function). Captures @func.def (the
// whole definition node) plus @func.name. C#/Python/Java/MATLAB/Scala are
// absent on purpose — their bundled tags query already emits function spans.
var funcSpanQuery = map[string]string{
	"ruby": `(method name: (identifier) @func.name) @func.def
(singleton_method name: (identifier) @func.name) @func.def`,
	"swift":  `(function_declaration (simple_identifier) @func.name) @func.def`,
	"kotlin": `(function_declaration (simple_identifier) @func.name) @func.def`,
	"php": `(function_definition (name) @func.name) @func.def
(method_declaration (name) @func.name) @func.def`,
	"perl": `(subroutine_declaration_statement (bareword) @func.name) @func.def`,
	"r":    `(binary_operator (identifier) @func.name (function_definition)) @func.def`,
}

// decisionQuery captures cyclomatic-complexity decision points as @decision:
// branch/loop/case nodes + short-circuit operators. Cyclomatic = 1 + the count
// contained in the innermost enclosing function span. Node names vary per
// grammar.
var decisionQuery = map[string]string{
	"rust":       `[(if_expression) (while_expression) (for_expression) (loop_expression) (match_arm) (binary_expression "&&") (binary_expression "||")] @decision`,
	"typescript": `[(if_statement) (while_statement) (for_statement) (for_in_statement) (do_statement) (switch_case) (catch_clause) (ternary_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"javascript": `[(if_statement) (while_statement) (for_statement) (for_in_statement) (do_statement) (switch_case) (catch_clause) (ternary_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"ruby":       `[(if) (elsif) (unless) (while) (until) (for) (when) (rescue) (conditional) (binary "&&") (binary "||")] @decision`,
	"swift":      `[(if_statement) (guard_statement) (while_statement) (for_statement) (switch_entry) (catch_block) (ternary_expression) (conjunction_expression) (disjunction_expression)] @decision`,
	"kotlin":     `[(if_expression) (while_statement) (do_while_statement) (for_statement) (when_entry) (catch_block) (conjunction_expression) (disjunction_expression)] @decision`,
	"c":          `[(if_statement) (while_statement) (for_statement) (do_statement) (case_statement) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"cpp":        `[(if_statement) (while_statement) (for_statement) (do_statement) (case_statement) (catch_clause) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"python":     `[(if_statement) (elif_clause) (for_statement) (while_statement) (except_clause) (conditional_expression) (boolean_operator)] @decision`,
	"java":       `[(if_statement) (for_statement) (enhanced_for_statement) (while_statement) (do_statement) (switch_label) (catch_clause) (ternary_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"csharp":     `[(if_statement) (for_statement) (foreach_statement) (while_statement) (do_statement) (switch_section) (catch_clause) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"php":        `[(if_statement) (else_if_clause) (for_statement) (foreach_statement) (while_statement) (do_statement) (case_statement) (catch_clause) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"perl":       `[(conditional_statement) (postfix_conditional_expression)] @decision`,
	"r":          `[(if_statement) (for_statement) (while_statement)] @decision`,
	"matlab":     `[(if_statement) (elseif_clause) (for_statement) (while_statement) (case_clause) (catch_clause)] @decision`,
	"scala":      `[(if_expression) (for_expression) (while_expression) (case_clause) (catch_clause)] @decision`,
}

// langState holds the concurrent-safe machinery for one language: a ParserPool
// (safe for concurrent Parse) plus compiled queries (safe for concurrent
// Execute). Built once per language on first use.
type langState struct {
	pool          *ts.ParserPool
	lang          *ts.Language
	tagsQuery     *ts.Query // bundled grammar tags; function spans for most languages
	spanQuery     *ts.Query // supplemental @func.def/@func.name; nil when none
	decisionQuery *ts.Query // @decision points; nil when none
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*langState{} // language -> *langState; nil = unsupported
)

// langFor lazily builds and caches the tree-sitter machinery for a language,
// or returns nil when the language isn't supported or its grammar is
// unavailable in this build.
func langFor(language string) *langState {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if ls, ok := cache[language]; ok {
		return ls
	}
	ls := buildLangState(language)
	cache[language] = ls
	return ls
}

func buildLangState(language string) *langState {
	sample, ok := detectFile[language]
	if !ok {
		return nil
	}
	entry := grammars.DetectLanguage(sample)
	if entry == nil {
		return nil
	}
	lang := entry.Language()
	if lang == nil {
		return nil
	}
	ls := &langState{lang: lang, pool: ts.NewParserPool(lang, ts.WithParserPoolTimeoutMicros(parseTimeoutMicros))}
	if tagsSrc := grammars.ResolveTagsQuery(*entry); tagsSrc != "" {
		if q, err := ts.NewQuery(tagsSrc, lang); err == nil {
			ls.tagsQuery = q
		}
	}
	if src := funcSpanQuery[language]; src != "" {
		if q, err := ts.NewQuery(src, lang); err == nil {
			ls.spanQuery = q
		}
	}
	if src := decisionQuery[language]; src != "" {
		if q, err := ts.NewQuery(src, lang); err == nil {
			ls.decisionQuery = q
		}
	}
	return ls
}

// funcSpan is a named function definition's byte span + 1-based line span.
type funcSpan struct {
	name               string
	start, end         uint32
	startLine, endLine uint32
}

func newFuncSpan(name string, n *ts.Node) funcSpan {
	return funcSpan{
		name: name, start: n.StartByte(), end: n.EndByte(),
		startLine: n.StartPoint().Row + 1, endLine: n.EndPoint().Row + 1,
	}
}

// SupportedLanguages returns the language identifiers Parse accepts, sorted.
func SupportedLanguages() []string {
	out := make([]string, 0, len(detectFile))
	for l := range detectFile {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// Parse computes cyclomatic and cognitive complexity for every function in
// src, for one of the supported non-Go languages (see [SupportedLanguages]).
//
// An unsupported or unavailable language returns a wrapped
// [codemetrics.ErrUnsupportedLanguage]. Parsing is best-effort: source that
// only partially parses yields metrics for the functions that were recovered;
// source whose parse times out or fails yields an empty slice and a nil error.
//
// Cognitive is nil for languages without a cognitive spec (currently Swift);
// Cyclomatic is always reported.
func Parse(language string, src []byte) ([]codemetrics.FunctionMetrics, error) {
	ls := langFor(language)
	if ls == nil {
		return nil, fmt.Errorf("%w: %q", codemetrics.ErrUnsupportedLanguage, language)
	}
	tree, err := ls.pool.Parse(src)
	if err != nil || tree == nil {
		return nil, nil // timeout / parse failure → best-effort empty
	}
	spans := collectFuncSpans(ls, tree, src)
	if len(spans) == 0 {
		return nil, nil
	}

	// Cyclomatic: 1 + decision points in the innermost enclosing span.
	decisions := make([]int, len(spans))
	if ls.decisionQuery != nil {
		for _, m := range ls.decisionQuery.Execute(tree) {
			for _, c := range m.Captures {
				if c.Name != "decision" {
					continue
				}
				if i := innermostFuncSpanIndex(spans, c.Node.StartByte()); i >= 0 {
					decisions[i]++
				}
			}
		}
	}

	cognitive := cognitiveComplexity(language, ls, tree, spans)

	out := make([]codemetrics.FunctionMetrics, 0, len(spans))
	for i, s := range spans {
		m := codemetrics.FunctionMetrics{
			Name:       s.name,
			Cyclomatic: 1 + decisions[i],
			StartLine:  int(s.startLine),
			EndLine:    int(s.endLine),
		}
		if cognitive != nil && i < len(cognitive) && cognitive[i] != nil {
			m.Cognitive = cognitive[i]
		}
		out = append(out, m)
	}
	return out, nil
}

// collectFuncSpans gathers function spans from the bundled tags query (most
// languages) plus the supplemental span query (ruby/swift/kotlin/php/perl/r).
func collectFuncSpans(ls *langState, tree *ts.Tree, src []byte) []funcSpan {
	var spans []funcSpan
	if ls.tagsQuery != nil {
		for _, m := range ls.tagsQuery.Execute(tree) {
			var name, kind string
			var defNode *ts.Node
			for _, c := range m.Captures {
				switch {
				case c.Name == "name":
					name = c.Text(src)
				case strings.HasPrefix(c.Name, "definition."):
					kind = c.Name[len("definition."):]
					defNode = c.Node
				}
			}
			if name == "" || defNode == nil {
				continue
			}
			switch kind {
			case "function", "method", "macro", "constructor":
				spans = append(spans, newFuncSpan(name, defNode))
			}
		}
	}
	if ls.spanQuery != nil {
		for _, m := range ls.spanQuery.Execute(tree) {
			var name string
			var defNode *ts.Node
			for _, c := range m.Captures {
				switch c.Name {
				case "func.name":
					name = c.Text(src)
				case "func.def":
					defNode = c.Node
				}
			}
			if name != "" && defNode != nil {
				spans = append(spans, newFuncSpan(name, defNode))
			}
		}
	}
	return spans
}

// innermostFuncSpanIndex returns the index of the smallest function span
// containing pos, or -1 if none does.
func innermostFuncSpanIndex(spans []funcSpan, pos uint32) int {
	best := -1
	bestSize := ^uint32(0)
	for i, s := range spans {
		if pos < s.start || pos >= s.end {
			continue
		}
		if size := s.end - s.start; size < bestSize {
			bestSize = size
			best = i
		}
	}
	return best
}
