package cli

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ansiSyntaxKeyword  = "\x1b[38;5;213m"
	ansiSyntaxString   = "\x1b[38;5;114m"
	ansiSyntaxComment  = "\x1b[38;5;244m"
	ansiSyntaxNumber   = "\x1b[38;5;208m"
	ansiSyntaxFunction = "\x1b[38;5;81m"
	ansiSyntaxProperty = "\x1b[38;5;44m"
	ansiSyntaxType     = "\x1b[38;5;221m"
	ansiSyntaxOperator = "\x1b[38;5;177m"
)

type previewSyntaxRules struct {
	keywords        map[string]bool
	lineComments    []string
	blockComments   bool
	backtickStrings bool
	markdown        bool
	enabled         bool
}

type previewHighlighter struct {
	rules          previewSyntaxRules
	inBlockComment bool
}

func newPreviewHighlighter(path string) *previewHighlighter {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))
	rules := previewSyntaxRules{}

	switch ext {
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".vue", ".svelte":
		rules = previewSyntaxRules{keywords: javascriptKeywords, lineComments: []string{"//"}, blockComments: true, backtickStrings: true, enabled: true}
	case ".go":
		rules = previewSyntaxRules{keywords: goKeywords, lineComments: []string{"//"}, blockComments: true, backtickStrings: true, enabled: true}
	case ".py":
		rules = previewSyntaxRules{keywords: pythonKeywords, lineComments: []string{"#"}, enabled: true}
	case ".rs":
		rules = previewSyntaxRules{keywords: rustKeywords, lineComments: []string{"//"}, blockComments: true, enabled: true}
	case ".java", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".swift", ".kt":
		rules = previewSyntaxRules{keywords: cFamilyKeywords, lineComments: []string{"//"}, blockComments: true, enabled: true}
	case ".sh", ".bash", ".zsh", ".ps1":
		rules = previewSyntaxRules{keywords: shellKeywords, lineComments: []string{"#"}, backtickStrings: true, enabled: true}
	case ".json":
		rules = previewSyntaxRules{keywords: configKeywords, enabled: true}
	case ".yaml", ".yml", ".toml", ".ini":
		rules = previewSyntaxRules{keywords: configKeywords, lineComments: []string{"#"}, enabled: true}
	case ".sql":
		rules = previewSyntaxRules{keywords: sqlKeywords, lineComments: []string{"--"}, blockComments: true, enabled: true}
	case ".css", ".scss":
		rules = previewSyntaxRules{keywords: cssKeywords, blockComments: true, enabled: true}
	case ".html", ".xml", ".svg":
		rules = previewSyntaxRules{keywords: htmlKeywords, blockComments: true, enabled: true}
	case ".md", ".mdx":
		rules = previewSyntaxRules{markdown: true, enabled: true}
	case ".mod":
		rules = previewSyntaxRules{keywords: goModuleKeywords, lineComments: []string{"//"}, enabled: true}
	}
	if base == "makefile" || base == "dockerfile" || base == "containerfile" {
		rules = previewSyntaxRules{keywords: shellKeywords, lineComments: []string{"#"}, enabled: true}
	}
	return &previewHighlighter{rules: rules}
}

func (h *previewHighlighter) highlight(line string) string {
	if !h.rules.enabled {
		return line
	}
	if h.rules.markdown {
		return highlightMarkdown(line)
	}

	var out strings.Builder
	for i := 0; i < len(line); {
		if h.inBlockComment {
			end := strings.Index(line[i:], "*/")
			if end < 0 {
				writeSyntaxColor(&out, ansiSyntaxComment, line[i:])
				break
			}
			end += i + 2
			writeSyntaxColor(&out, ansiSyntaxComment, line[i:end])
			h.inBlockComment = false
			i = end
			continue
		}

		if marker := matchingLineComment(line[i:], h.rules.lineComments); marker != "" {
			writeSyntaxColor(&out, ansiSyntaxComment, line[i:])
			break
		}
		if h.rules.blockComments && strings.HasPrefix(line[i:], "/*") {
			end := strings.Index(line[i+2:], "*/")
			if end < 0 {
				writeSyntaxColor(&out, ansiSyntaxComment, line[i:])
				h.inBlockComment = true
				break
			}
			end += i + 4
			writeSyntaxColor(&out, ansiSyntaxComment, line[i:end])
			i = end
			continue
		}

		b := line[i]
		if b == '\'' || b == '"' || b == '`' && h.rules.backtickStrings {
			end := quotedTokenEnd(line, i, b)
			writeSyntaxColor(&out, ansiSyntaxString, line[i:end])
			i = end
			continue
		}
		if isASCIIDigit(b) {
			end := i + 1
			for end < len(line) && (isASCIIDigit(line[end]) || strings.ContainsRune("._xXabcdefABCDEF", rune(line[end]))) {
				end++
			}
			writeSyntaxColor(&out, ansiSyntaxNumber, line[i:end])
			i = end
			continue
		}
		if isIdentifierStart(b) {
			end := i + 1
			for end < len(line) && isIdentifierPart(line[end]) {
				end++
			}
			token := line[i:end]
			next := nextNonSpace(line, end)
			switch {
			case h.rules.keywords[strings.ToLower(token)]:
				writeSyntaxColor(&out, ansiSyntaxKeyword, token)
			case next == '(':
				writeSyntaxColor(&out, ansiSyntaxFunction, token)
			case next == ':':
				writeSyntaxColor(&out, ansiSyntaxProperty, token)
			case unicode.IsUpper(rune(token[0])):
				writeSyntaxColor(&out, ansiSyntaxType, token)
			default:
				out.WriteString(token)
			}
			i = end
			continue
		}
		if strings.ContainsRune("=+-*/%<>!&|?:", rune(b)) {
			writeSyntaxColor(&out, ansiSyntaxOperator, line[i:i+1])
			i++
			continue
		}
		if b >= utf8.RuneSelf {
			_, size := utf8.DecodeRuneInString(line[i:])
			out.WriteString(line[i : i+size])
			i += size
			continue
		}
		out.WriteByte(b)
		i++
	}
	return out.String()
}

func highlightMarkdown(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(trimmed)]
	if strings.HasPrefix(trimmed, "#") {
		return indent + ansiBold + ansiSyntaxKeyword + trimmed + ansiReset
	}
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "> ") {
		return indent + ansiSyntaxOperator + trimmed[:2] + ansiReset + highlightInlineCode(trimmed[2:])
	}
	return indent + highlightInlineCode(trimmed)
}

func highlightInlineCode(line string) string {
	var out strings.Builder
	for {
		start := strings.IndexByte(line, '`')
		if start < 0 {
			out.WriteString(line)
			break
		}
		out.WriteString(line[:start])
		end := strings.IndexByte(line[start+1:], '`')
		if end < 0 {
			out.WriteString(line[start:])
			break
		}
		end += start + 2
		writeSyntaxColor(&out, ansiSyntaxString, line[start:end])
		line = line[end:]
	}
	return out.String()
}

func writeSyntaxColor(out *strings.Builder, color, token string) {
	out.WriteString(color)
	out.WriteString(token)
	out.WriteString(ansiReset)
}

func matchingLineComment(value string, markers []string) string {
	for _, marker := range markers {
		if strings.HasPrefix(value, marker) {
			return marker
		}
	}
	return ""
}

func quotedTokenEnd(line string, start int, quote byte) int {
	escaped := false
	for i := start + 1; i < len(line); i++ {
		if escaped {
			escaped = false
			continue
		}
		if line[i] == '\\' {
			escaped = true
			continue
		}
		if line[i] == quote {
			return i + 1
		}
	}
	return len(line)
}

func nextNonSpace(line string, start int) byte {
	for i := start; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return line[i]
		}
	}
	return 0
}

func isIdentifierStart(b byte) bool {
	return b == '_' || b == '$' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isIdentifierPart(b byte) bool {
	return isIdentifierStart(b) || isASCIIDigit(b)
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func keywordSet(words string) map[string]bool {
	set := make(map[string]bool)
	for _, word := range strings.Fields(words) {
		set[strings.ToLower(word)] = true
	}
	return set
}

var (
	javascriptKeywords = keywordSet("import from export default const let var function return if else for while do switch case break continue class extends new this super async await try catch finally throw typeof instanceof in of true false null undefined interface type enum implements private public protected static readonly declare namespace as satisfies yield delete void")
	goKeywords         = keywordSet("break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var true false nil iota")
	pythonKeywords     = keywordSet("and as assert async await break class continue def del elif else except false finally for from global if import in is lambda none nonlocal not or pass raise return true try while with yield match case")
	rustKeywords       = keywordSet("as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while")
	cFamilyKeywords    = keywordSet("abstract as auto bool break case catch char class const continue default delete do double else enum extends false final finally float for foreach if implements import in int interface long namespace new nil null override package private protected public return short signed sizeof static string struct super switch this throw throws true try typedef uint union unsigned using var virtual void volatile while")
	shellKeywords      = keywordSet("if then else elif fi for while until do done case esac function in select time coproc export local readonly declare typeset true false")
	configKeywords     = keywordSet("true false null nil yes no on off")
	sqlKeywords        = keywordSet("select from where insert into update delete create alter drop table index join inner left right full outer on as and or not null true false group by order having limit offset union all distinct values set begin commit rollback primary foreign key references")
	cssKeywords        = keywordSet("important inherit initial unset auto none transparent block inline flex grid absolute relative fixed sticky media supports keyframes from to")
	htmlKeywords       = keywordSet("doctype html head body script style template div span main section article nav header footer link meta title class id")
	goModuleKeywords   = keywordSet("module go toolchain require replace exclude retract use")
)
