package treesitter

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

// funcSpanQuery supplements the grammar's bundled tags query for languages whose
// tags query doesn't expose a function span. Captures @func.def + @func.name.
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
// contained in the innermost enclosing function span.
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
