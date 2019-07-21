package token

type TokenType string

const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"

	// Identifiers + literals
	IDENT  = "IDENT"  // add, foobar, x, y, ...
	INT    = "INT"    // 1343456
	STRING = "STRING" // "foobar"
	REGEXP = "REGEXP" // /^v1.0/

	BANG = "!"
	DOT  = "."

	// Comparison Operators
	EQ        = "=="
	NOT_EQ    = "!="
	RE_EQ     = "=~"
	RE_NOT_EQ = "!~"
	CONTAINS  = "@>"

	// Boolean Operators
	AND = "&&"
	OR  = "||"

	// Delimiters
	COMMA    = ","
	LPAREN   = "("
	RPAREN   = ")"
	LBRACKET = "["
	RBRACKET = "]"

	// Keywords
	TRUE  = "TRUE"
	FALSE = "FALSE"
)

type Token struct {
	Type    TokenType
	Literal string
}

var keywords = map[string]TokenType{
	"true":  TRUE,
	"false": FALSE,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
