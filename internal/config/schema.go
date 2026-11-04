package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Column describes a column in the supported CREATE TABLE subset.
type Column struct {
	Name      string
	Type      string
	Length    int
	Precision int
	Scale     int
	Nullable  bool
}

// TableDefinition retains declared key order. UniqueKeys includes the primary key.
// Schema is empty when the definition is not explicitly schema-qualified.
type TableDefinition struct {
	Schema     string
	Name       string
	Columns    map[string]Column
	UniqueKeys [][]string
}

type schemaToken struct {
	text string
	pos  int
}

type schemaKey struct {
	columns []string
	primary bool
}

type schemaParser struct {
	tokens       []schemaToken
	index        int
	end          int
	keys         []schemaKey
	explicitNull map[string]bool
	constraints  map[string]bool
	primarySeen  bool
}

// parseSchema parses exactly one CREATE TABLE, without executing SQL.
func parseSchema(raw []byte) (*TableDefinition, error) {
	tokens, err := scanSchema(raw)
	if err != nil {
		return nil, err
	}
	p := schemaParser{
		tokens: tokens, end: len(raw),
		explicitNull: make(map[string]bool), constraints: make(map[string]bool),
	}
	return p.parse()
}

func schemaLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func schemaDigit(c byte) bool { return c >= '0' && c <= '9' }

func scanSchema(raw []byte) ([]schemaToken, error) {
	var tokens []schemaToken
	for i := 0; i < len(raw); {
		switch {
		case raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\r' || raw[i] == '\n' || raw[i] == '\f':
			i++
		case i+1 < len(raw) && raw[i] == '-' && raw[i+1] == '-':
			i += 2
			for i < len(raw) && raw[i] != '\n' && raw[i] != '\r' {
				i++
			}
		case i+1 < len(raw) && raw[i] == '/' && raw[i+1] == '*':
			start := i
			i += 2
			depth := 1
			for i < len(raw) && depth > 0 {
				if i+1 < len(raw) && raw[i] == '/' && raw[i+1] == '*' {
					depth++
					i += 2
				} else if i+1 < len(raw) && raw[i] == '*' && raw[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return nil, configError(fmt.Sprintf("schema at byte %d: unterminated block comment", start))
			}
		case schemaLetter(raw[i]):
			start := i
			i++
			for i < len(raw) && (schemaLetter(raw[i]) || schemaDigit(raw[i])) {
				i++
			}
			tokens = append(tokens, schemaToken{string(raw[start:i]), start})
		case schemaDigit(raw[i]):
			start := i
			for i < len(raw) && schemaDigit(raw[i]) {
				i++
			}
			tokens = append(tokens, schemaToken{string(raw[start:i]), start})
		case strings.ContainsRune("(),.;", rune(raw[i])):
			tokens = append(tokens, schemaToken{string(raw[i]), i})
			i++
		default:
			return nil, configError(fmt.Sprintf("schema at byte %d: unsupported character %q (quoted identifiers and expressions are unsupported)", i, raw[i]))
		}
	}
	return tokens, nil
}

func (p *schemaParser) peek() string {
	if p.index == len(p.tokens) {
		return ""
	}
	return p.tokens[p.index].text
}

func (p *schemaParser) take(text string) bool {
	if strings.EqualFold(p.peek(), text) && p.peek() != "" {
		p.index++
		return true
	}
	return false
}

func (p *schemaParser) fail(format string, args ...any) error {
	pos := p.end
	if p.index < len(p.tokens) {
		pos = p.tokens[p.index].pos
	}
	return configError(fmt.Sprintf("schema at byte %d: %s", pos, fmt.Sprintf(format, args...)))
}

func (p *schemaParser) expect(text string) error {
	if !p.take(text) {
		return p.fail("expected %s, got %q", text, p.peek())
	}
	return nil
}

func (p *schemaParser) identifier(context string) (string, error) {
	name := p.peek()
	if name == "" || !schemaLetter(name[0]) || name != strings.ToLower(name) {
		return "", p.fail("%s: expected simple lowercase unquoted identifier, got %q", context, name)
	}
	p.index++
	return name, nil
}

func (p *schemaParser) parse() (*TableDefinition, error) {
	for _, keyword := range []string{"CREATE", "TABLE"} {
		if err := p.expect(keyword); err != nil {
			return nil, err
		}
	}
	if p.take("IF") {
		if err := p.expect("NOT"); err != nil {
			return nil, err
		}
		if err := p.expect("EXISTS"); err != nil {
			return nil, err
		}
	}
	name, err := p.identifier("table name")
	if err != nil {
		return nil, err
	}
	table := &TableDefinition{Name: name, Columns: make(map[string]Column)}
	if p.take(".") {
		table.Schema = name
		table.Name, err = p.identifier("table name")
		if err != nil {
			return nil, err
		}
	}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	for {
		switch strings.ToUpper(p.peek()) {
		case "CONSTRAINT", "PRIMARY", "UNIQUE":
			err = p.tableKey()
		default:
			err = p.column(table)
		}
		if err != nil {
			return nil, err
		}
		if p.take(")") {
			break
		}
		if err := p.expect(","); err != nil {
			return nil, err
		}
	}
	p.take(";")
	if p.peek() != "" {
		return nil, p.fail("unsupported trailing syntax %q", p.peek())
	}
	if len(table.Columns) == 0 {
		return nil, p.fail("table %s must have at least one column", table.Name)
	}
	for _, key := range p.keys {
		for _, name := range key.columns {
			column, exists := table.Columns[name]
			if !exists {
				return nil, p.fail("key references missing column %q", name)
			}
			if key.primary {
				if p.explicitNull[name] {
					return nil, p.fail("primary key column %q cannot explicitly allow NULL", name)
				}
				column.Nullable = false
				table.Columns[name] = column
			}
		}
		table.UniqueKeys = append(table.UniqueKeys, key.columns)
	}
	return table, nil
}

func (p *schemaParser) number(context string, minimum, maximum int) (int, error) {
	text := p.peek()
	value, err := strconv.Atoi(text)
	if err != nil || text == "" || !schemaDigit(text[0]) || value < minimum || value > maximum {
		return 0, p.fail("%s must be an integer in %d..%d, got %q", context, minimum, maximum, text)
	}
	p.index++
	return value, nil
}

func (p *schemaParser) column(table *TableDefinition) error {
	name, err := p.identifier("column name")
	if err != nil {
		return err
	}
	if _, exists := table.Columns[name]; exists {
		return p.fail("duplicate column %q", name)
	}
	column := Column{Name: name, Nullable: true}
	typeName := strings.ToUpper(p.peek())
	if typeName == "" {
		return p.fail("column %q: missing type", name)
	}
	p.index++
	switch typeName {
	case "UUID", "BOOLEAN", "JSONB":
		column.Type = strings.ToLower(typeName)
	case "INT", "INTEGER":
		column.Type = "integer"
	case "VARCHAR", "DECIMAL", "NUMERIC":
		if err := p.expect("("); err != nil {
			return err
		}
		if typeName == "VARCHAR" {
			column.Type = "varchar"
			column.Length, err = p.number("column "+name+" VARCHAR length", 1, 10485760)
		} else {
			column.Type = "numeric"
			column.Precision, err = p.number("column "+name+" numeric precision", 1, 1000)
			if err == nil {
				err = p.expect(",")
			}
			if err == nil {
				column.Scale, err = p.number("column "+name+" numeric scale", 0, column.Precision)
			}
		}
		if err != nil {
			return err
		}
		if err := p.expect(")"); err != nil {
			return err
		}
	case "TIMESTAMP":
		column.Type = "timestamp"
		if p.take("WITHOUT") {
			if err := p.expect("TIME"); err != nil {
				return err
			}
			if err := p.expect("ZONE"); err != nil {
				return err
			}
		}
	default:
		return p.fail("column %q: unsupported type %q", name, typeName)
	}
	seenNull, seenUnique, seenPrimary := false, false, false
	for p.peek() != "," && p.peek() != ")" && p.peek() != "" {
		switch {
		case p.take("NOT"):
			if err := p.expect("NULL"); err != nil {
				return err
			}
			if seenNull {
				return p.fail("column %q: repeated or contradictory nullability modifier", name)
			}
			seenNull = true
			column.Nullable = false
		case p.take("NULL"):
			if seenNull {
				return p.fail("column %q: repeated or contradictory nullability modifier", name)
			}
			seenNull = true
			p.explicitNull[name] = true
		case p.take("PRIMARY"):
			if err := p.expect("KEY"); err != nil {
				return err
			}
			if seenPrimary || p.primarySeen {
				return p.fail("column %q: multiple primary keys are unsupported", name)
			}
			seenPrimary, p.primarySeen = true, true
			p.keys = append(p.keys, schemaKey{[]string{name}, true})
		case p.take("UNIQUE"):
			if seenUnique {
				return p.fail("column %q: repeated UNIQUE modifier", name)
			}
			seenUnique = true
			p.keys = append(p.keys, schemaKey{columns: []string{name}})
		default:
			return p.fail("column %q: unsupported modifier %q", name, p.peek())
		}
	}
	table.Columns[name] = column
	return nil
}

func (p *schemaParser) tableKey() error {
	if p.take("CONSTRAINT") {
		name, err := p.identifier("constraint name")
		if err != nil {
			return err
		}
		if p.constraints[name] {
			return p.fail("duplicate constraint name %q", name)
		}
		p.constraints[name] = true
	}
	primary := p.take("PRIMARY")
	if primary {
		if err := p.expect("KEY"); err != nil {
			return err
		}
		if p.primarySeen {
			return p.fail("multiple primary keys are unsupported")
		}
		p.primarySeen = true
	} else if err := p.expect("UNIQUE"); err != nil {
		return err
	}
	if err := p.expect("("); err != nil {
		return err
	}
	var columns []string
	seen := make(map[string]bool)
	for {
		name, err := p.identifier("key column")
		if err != nil {
			return err
		}
		if seen[name] {
			return p.fail("duplicate key column %q", name)
		}
		seen[name] = true
		columns = append(columns, name)
		if p.take(")") {
			break
		}
		if err := p.expect(","); err != nil {
			return err
		}
	}
	p.keys = append(p.keys, schemaKey{columns, primary})
	return nil
}
