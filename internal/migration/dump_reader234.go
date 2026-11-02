package migration

// This reader recognizes a deliberately bounded MySQL dump grammar. It never
// executes statements, expands functions, evaluates variables, or contacts a DB.
import (
	"bytes"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxDumpBytes234   = 64 << 20
	maxDumpRows234    = 250000
	maxDumpColumns234 = 512
	maxDumpTables234  = 256
	maxDumpToken234   = 1 << 20
	maxDumpCells234   = 2000000
)

type dumpToken234 struct {
	kind   byte
	text   string
	offset int
}
type dumpSyntax234 struct {
	offset int
	code   string
}

func (e dumpSyntax234) Error() string { return "unsupported or invalid SQL backup" }

type dumpLexer234 struct {
	raw    []byte
	pos    int
	peeked *dumpToken234
	tokens int
}

var dumpIdentifier234 = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
var dumpNumber234 = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
var dumpHex234 = regexp.MustCompile(`^0[xX][0-9A-Fa-f]+$`)
var dumpAlterComment234 = regexp.MustCompile("(?is)^ALTER\\s+TABLE\\s+(?:`[A-Za-z0-9_]{1,64}`|[A-Za-z0-9_]{1,64})\\s+(?:DISABLE|ENABLE)\\s+KEYS\\s*;?$")

func (l *dumpLexer234) bad(code string) error { return dumpSyntax234{l.pos, code} }
func space234(b byte) bool                    { return b == ' ' || b == '\r' || b == '\n' || b == '\t' || b == '\f' }
func word234(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '$' || b == '@'
}
func (l *dumpLexer234) next() (dumpToken234, error) {
	if l.peeked != nil {
		t := *l.peeked
		l.peeked = nil
		return t, nil
	}
	for l.pos < len(l.raw) {
		if space234(l.raw[l.pos]) {
			l.pos++
			continue
		}
		if l.raw[l.pos] == '#' || l.pos+1 < len(l.raw) && l.raw[l.pos] == '-' && l.raw[l.pos+1] == '-' && (l.pos+2 == len(l.raw) || space234(l.raw[l.pos+2])) {
			for l.pos < len(l.raw) && l.raw[l.pos] != '\n' {
				l.pos++
			}
			continue
		}
		if l.pos+1 < len(l.raw) && l.raw[l.pos] == '/' && l.raw[l.pos+1] == '*' {
			start := l.pos
			l.pos += 2
			end := bytes.Index(l.raw[l.pos:], []byte("*/"))
			if end < 0 {
				return dumpToken234{}, l.bad("unterminated_comment")
			}
			body := l.raw[l.pos : l.pos+end]
			l.pos += end + 2
			if len(body) > 0 && (body[0] == '!' || len(body) > 1 && body[0] == 'M' && body[1] == '!') {
				if body[0] == 'M' {
					body = body[1:]
				}
				body = body[1:]
				for len(body) > 0 && body[0] >= '0' && body[0] <= '9' {
					body = body[1:]
				}
				v := strings.TrimSpace(string(body))
				u := strings.ToUpper(v)
				if strings.HasPrefix(u, "SET ") {
					if e := validateSet234([]byte(v)); e != nil {
						return dumpToken234{}, dumpSyntax234{start, "unsupported_executable_comment"}
					}
				} else if !dumpAlterComment234.MatchString(v) {
					return dumpToken234{}, dumpSyntax234{start, "unsupported_executable_comment"}
				}
			}
			continue
		}
		break
	}
	if l.pos == len(l.raw) {
		return dumpToken234{kind: 'e', offset: l.pos}, nil
	}
	l.tokens++
	if l.tokens > 8000000 {
		return dumpToken234{}, l.bad("too_many_tokens")
	}
	start := l.pos
	b := l.raw[l.pos]
	l.pos++
	if b == '\'' || b == '"' || b == '`' {
		out := make([]byte, 0, 32)
		for l.pos < len(l.raw) {
			c := l.raw[l.pos]
			l.pos++
			if c == b {
				if l.pos < len(l.raw) && l.raw[l.pos] == b {
					out = append(out, b)
					l.pos++
				} else {
					kind := byte('s')
					if b == '`' {
						kind = 'i'
					}
					return dumpToken234{kind, string(out), start}, nil
				}
			} else if c == '\\' && b != '`' {
				if l.pos == len(l.raw) {
					return dumpToken234{}, l.bad("unterminated_literal")
				}
				esc := l.raw[l.pos]
				l.pos++
				switch esc {
				case '0':
					c = 0
				case 'b':
					c = '\b'
				case 'n':
					c = '\n'
				case 'r':
					c = '\r'
				case 't':
					c = '\t'
				case 'Z':
					c = 26
				case '\\', '\'', '"':
					c = esc
				case '%', '_':
					out = append(out, '\\')
					c = esc
				default:
					c = esc
				}
				out = append(out, c)
			} else {
				out = append(out, c)
			}
			if l.pos-start > maxDumpToken234 || len(out) > maxDumpToken234 {
				return dumpToken234{}, l.bad("token_too_large")
			}
		}
		return dumpToken234{}, l.bad("unterminated_literal")
	}
	numeric := b >= '0' && b <= '9' || b == '+' || b == '-' || b == '.' && l.pos < len(l.raw) && l.raw[l.pos] >= '0' && l.raw[l.pos] <= '9'
	if word234(b) || numeric {
		for l.pos < len(l.raw) && (word234(l.raw[l.pos]) || numeric && strings.ContainsRune(".+-", rune(l.raw[l.pos]))) {
			l.pos++
			if l.pos-start > maxDumpToken234 {
				return dumpToken234{}, l.bad("token_too_large")
			}
		}
		text := string(l.raw[start:l.pos])
		kind := byte('w')
		if dumpNumber234.MatchString(text) {
			kind = 'n'
		} else if dumpHex234.MatchString(text) {
			kind = 'h'
		}
		return dumpToken234{kind, text, start}, nil
	}
	if strings.ContainsRune("(),;.=", rune(b)) {
		return dumpToken234{kind: b, text: string(b), offset: start}, nil
	}
	return dumpToken234{}, l.bad("unsupported_character")
}
func (l *dumpLexer234) peek() (dumpToken234, error) {
	t, e := l.next()
	if e == nil {
		l.peeked = &t
	}
	return t, e
}
func (l *dumpLexer234) expect(value string) error {
	t, e := l.next()
	if e != nil {
		return e
	}
	if !strings.EqualFold(t.text, value) {
		return dumpSyntax234{t.offset, "unexpected_token"}
	}
	return nil
}
func (l *dumpLexer234) ident() (string, error) {
	t, e := l.next()
	if e != nil {
		return "", e
	}
	if (t.kind != 'w' && t.kind != 'i') || !dumpIdentifier234.MatchString(t.text) {
		return "", dumpSyntax234{t.offset, "invalid_identifier"}
	}
	return t.text, nil
}

// SET statements are metadata only. Reject modes that would change our literal
// interpretation, and do not accept statement separators or hidden DML.
func validateSet234(raw []byte) error {
	forbiddenMode := func(s string) bool {
		s = strings.ToUpper(s)
		return strings.Contains(s, "NO_BACKSLASH_ESCAPES") || strings.Contains(s, "ANSI_QUOTES")
	}
	if forbiddenMode(string(raw)) {
		return ErrValidation
	}
	l := dumpLexer234{raw: raw}
	first, e := l.next()
	if e != nil || !strings.EqualFold(first.text, "SET") {
		return ErrValidation
	}
	ended := false
	for {
		t, e := l.next()
		if e != nil {
			return e
		}
		if t.kind == 'e' {
			return nil
		}
		if ended {
			return ErrValidation
		}
		if t.kind == ';' {
			ended = true
			continue
		}
		if t.kind == 's' && forbiddenMode(t.text) {
			return ErrValidation
		}
		if t.kind == 'h' {
			encoded := t.text[2:]
			if len(encoded)%2 != 0 {
				encoded = "0" + encoded
			}
			decoded, e := hex.DecodeString(encoded)
			if e != nil || forbiddenMode(string(decoded)) {
				return ErrValidation
			}
		}
		if t.kind == 'w' && strings.EqualFold(t.text, "X") {
			literal, e := l.peek()
			if e != nil {
				return e
			}
			if literal.kind == 's' {
				l.next()
				decoded, e := hex.DecodeString(literal.text)
				if e != nil || forbiddenMode(string(decoded)) {
					return ErrValidation
				}
			}
		}
		if t.kind == 'w' {
			switch strings.ToUpper(t.text) {
			case "INSERT", "UPDATE", "DELETE", "REPLACE", "SELECT", "CALL", "LOAD", "DROP", "CREATE", "ALTER", "ROLLBACK":
				return ErrValidation
			}
		}
	}
}

type dumpCell234 struct {
	text string
	null bool
}
type dumpRow234 map[string]dumpCell234
type dumpTableData234 struct {
	name    string
	columns []string
	rows    []dumpRow234
	count   int
}
type dumpReader234 struct {
	l          dumpLexer234
	tables     map[string]*dumpTableData234
	rows       int
	statements int
	cells      int
}

var dumpKeepColumns234 = map[string]bool{}

func init() {
	for _, c := range strings.Fields("uid money codename addtime deposit id name trade_no out_trade_no type status endtime version tid combine refundmoney realmoney getmoney refund_no out_refund_no biz_no costmoney") {
		dumpKeepColumns234[c] = true
	}
}
func keepRows234(name string) bool {
	for _, s := range []string{"user", "type", "order", "refundorder", "transfer", "settle", "psorder", "psinternal", "internal_transfer", "suborder"} {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

func readDump234(raw []byte) (map[string]*dumpTableData234, error) {
	r := dumpReader234{l: dumpLexer234{raw: raw}, tables: map[string]*dumpTableData234{}}
	for {
		t, e := r.l.next()
		if e != nil {
			return r.tables, e
		}
		if t.kind == 'e' {
			return r.tables, nil
		}
		if t.kind == ';' {
			continue
		}
		r.statements++
		if r.statements > 500000 {
			return r.tables, r.l.bad("too_many_statements")
		}
		if t.kind != 'w' {
			return r.tables, dumpSyntax234{t.offset, "unsupported_statement"}
		}
		switch strings.ToUpper(t.text) {
		case "CREATE":
			e = r.create()
		case "INSERT":
			e = r.insert()
		case "DROP":
			e = r.drop()
		case "SET":
			e = r.set()
		case "LOCK":
			if e = r.l.expect("TABLES"); e == nil {
				e = r.skipMetadata()
			}
		case "UNLOCK":
			if e = r.l.expect("TABLES"); e == nil {
				e = r.l.expect(";")
			}
		case "ALTER":
			e = r.alter()
		case "USE":
			if _, e = r.l.ident(); e == nil {
				e = r.l.expect(";")
			}
		case "START":
			if e = r.l.expect("TRANSACTION"); e == nil {
				e = r.l.expect(";")
			}
		case "BEGIN", "COMMIT":
			e = r.l.expect(";")
		default:
			e = dumpSyntax234{t.offset, "unsupported_statement"}
		}
		if e != nil {
			return r.tables, e
		}
	}
}
func (r *dumpReader234) skipMetadata() error {
	for count := 0; count < 100000; count++ {
		t, e := r.l.next()
		if e != nil {
			return e
		}
		if t.kind == ';' {
			return nil
		}
		if t.kind == 'w' {
			switch strings.ToUpper(t.text) {
			case "AS", "SELECT", "INSERT", "UPDATE", "DELETE", "REPLACE", "LOAD", "CALL", "ROLLBACK":
				return r.l.bad("unsupported_metadata_statement")
			}
		}
		if t.kind == 'e' {
			return r.l.bad("truncated_statement")
		}
	}
	return r.l.bad("metadata_too_large")
}
func (r *dumpReader234) set() error {
	start := r.l.pos - len("SET")
	if r.l.peeked != nil {
		return r.l.bad("unexpected_token")
	}
	for {
		t, e := r.l.next()
		if e != nil {
			return e
		}
		if t.kind == 'e' {
			return r.l.bad("truncated_statement")
		}
		if t.kind == ';' {
			return validateSet234(r.l.raw[start:r.l.pos])
		}
	}
}
func (r *dumpReader234) drop() error {
	if e := r.l.expect("TABLE"); e != nil {
		return e
	}
	t, e := r.l.peek()
	if e != nil {
		return e
	}
	if strings.EqualFold(t.text, "IF") {
		r.l.next()
		if e = r.l.expect("EXISTS"); e != nil {
			return e
		}
	}
	name, e := r.l.ident()
	if e != nil {
		return e
	}
	if _, ok := r.tables[name]; ok {
		return r.l.bad("drop_after_create")
	}
	return r.l.expect(";")
}
func (r *dumpReader234) alter() error {
	if e := r.l.expect("TABLE"); e != nil {
		return e
	}
	if _, e := r.l.ident(); e != nil {
		return e
	}
	t, e := r.l.next()
	if e != nil {
		return e
	}
	if !strings.EqualFold(t.text, "ENABLE") && !strings.EqualFold(t.text, "DISABLE") {
		return r.l.bad("unsupported_alter")
	}
	if e = r.l.expect("KEYS"); e != nil {
		return e
	}
	return r.l.expect(";")
}
func (r *dumpReader234) create() error {
	k, e := r.l.next()
	if e != nil {
		return e
	}
	if strings.EqualFold(k.text, "DATABASE") || strings.EqualFold(k.text, "SCHEMA") {
		return r.skipMetadata()
	}
	if !strings.EqualFold(k.text, "TABLE") {
		return r.l.bad("unsupported_create")
	}
	t, e := r.l.peek()
	if e != nil {
		return e
	}
	if strings.EqualFold(t.text, "IF") {
		r.l.next()
		if e = r.l.expect("NOT"); e != nil {
			return e
		}
		if e = r.l.expect("EXISTS"); e != nil {
			return e
		}
	}
	name, e := r.l.ident()
	if e != nil {
		return e
	}
	if len(r.tables) >= maxDumpTables234 {
		return r.l.bad("too_many_tables")
	}
	if _, ok := r.tables[name]; ok {
		return r.l.bad("duplicate_table")
	}
	if e = r.l.expect("("); e != nil {
		return e
	}
	table := &dumpTableData234{name: name}
	seen := map[string]bool{}
	for {
		first, e := r.l.next()
		if e != nil {
			return e
		}
		if first.kind == ')' {
			return r.l.bad("empty_or_trailing_schema_column")
		}
		if (first.kind != 'w' && first.kind != 'i') || !dumpIdentifier234.MatchString(first.text) {
			return r.l.bad("invalid_column")
		}
		constraint := false
		if first.kind == 'w' {
			switch strings.ToUpper(first.text) {
			case "PRIMARY", "UNIQUE", "KEY", "INDEX", "CONSTRAINT", "CHECK", "FOREIGN", "FULLTEXT", "SPATIAL":
				constraint = true
			}
		}
		if !constraint {
			c := strings.ToLower(first.text)
			if seen[c] || len(table.columns) >= maxDumpColumns234 {
				return r.l.bad("duplicate_or_excess_columns")
			}
			seen[c] = true
			table.columns = append(table.columns, c)
		}
		depth := 0
		haveType := constraint
		for {
			t, e := r.l.next()
			if e != nil {
				return e
			}
			if t.kind == 'e' || t.kind == ';' {
				return r.l.bad("truncated_schema")
			}
			if t.kind == '(' {
				depth++
				if depth > 16 {
					return r.l.bad("schema_depth")
				}
			} else if t.kind == ')' {
				if depth == 0 {
					if !haveType {
						return r.l.bad("missing_column_type")
					}
					r.tables[name] = table
					return r.skipMetadata()
				}
				depth--
			} else if t.kind == ',' && depth == 0 {
				if !haveType {
					return r.l.bad("missing_column_type")
				}
				break
			}
			if t.kind == 'w' {
				haveType = true
			}
		}
	}
}
func (r *dumpReader234) insert() error {
	if e := r.l.expect("INTO"); e != nil {
		return e
	}
	name, e := r.l.ident()
	if e != nil {
		return e
	}
	table, ok := r.tables[name]
	if !ok {
		return r.l.bad("insert_without_schema")
	}
	columns := table.columns
	t, e := r.l.peek()
	if e != nil {
		return e
	}
	if t.kind == '(' {
		r.l.next()
		columns = nil
		seen := map[string]bool{}
		valid := map[string]bool{}
		for _, c := range table.columns {
			valid[c] = true
		}
		for {
			c, e := r.l.ident()
			if e != nil {
				return e
			}
			c = strings.ToLower(c)
			if seen[c] || !valid[c] || len(columns) >= maxDumpColumns234 {
				return r.l.bad("invalid_insert_columns")
			}
			seen[c] = true
			columns = append(columns, c)
			sep, e := r.l.next()
			if e != nil {
				return e
			}
			if sep.kind == ')' {
				break
			}
			if sep.kind != ',' {
				return r.l.bad("invalid_insert_columns")
			}
		}
	}
	if e = r.l.expect("VALUES"); e != nil {
		return e
	}
	for {
		if e = r.l.expect("("); e != nil {
			return e
		}
		row := dumpRow234{}
		for i, c := range columns {
			if r.cells >= maxDumpCells234 {
				return r.l.bad("too_many_cells")
			}
			r.cells++
			if i > 0 {
				if e = r.l.expect(","); e != nil {
					return e
				}
			}
			t, e := r.l.next()
			if e != nil {
				return e
			}
			v := dumpCell234{}
			switch {
			case t.kind == 's' || t.kind == 'n':
				v.text = t.text
			case t.kind == 'h':
				encoded := t.text[2:]
				if len(encoded)%2 != 0 {
					encoded = "0" + encoded
				}
				decoded, err := hex.DecodeString(encoded)
				if err != nil {
					return r.l.bad("invalid_hex_literal")
				}
				v.text = string(decoded)
			case t.kind == 'w' && (strings.EqualFold(t.text, "X") || strings.EqualFold(t.text, "_binary") || strings.EqualFold(t.text, "_utf8") || strings.EqualFold(t.text, "_utf8mb4")):
				literal, err := r.l.next()
				if err != nil {
					return err
				}
				if literal.kind != 's' {
					return r.l.bad("nonliteral_insert_value")
				}
				v.text = literal.text
				if strings.EqualFold(t.text, "X") {
					decoded, err := hex.DecodeString(literal.text)
					if err != nil {
						return r.l.bad("invalid_hex_literal")
					}
					v.text = string(decoded)
				}
			case t.kind == 'w' && strings.EqualFold(t.text, "NULL"):
				v.null = true
			default:
				return r.l.bad("nonliteral_insert_value")
			}
			if keepRows234(name) && dumpKeepColumns234[c] {
				row[c] = v
			}
		}
		if e = r.l.expect(")"); e != nil {
			return e
		}
		r.rows++
		if r.rows > maxDumpRows234 {
			return r.l.bad("too_many_rows")
		}
		table.count++
		if keepRows234(name) {
			table.rows = append(table.rows, row)
		}
		t, e := r.l.next()
		if e != nil {
			return e
		}
		if t.kind == ';' {
			return nil
		}
		if t.kind != ',' {
			return r.l.bad("invalid_insert_ending")
		}
	}
}

func validDumpBytes234(raw []byte) bool {
	return len(raw) > 0 && len(raw) <= maxDumpBytes234 && utf8.Valid(raw) && !bytes.Contains(raw, []byte{0})
}
