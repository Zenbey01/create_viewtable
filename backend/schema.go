package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type ForeignKey struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

type Column struct {
	Name string      `json:"name"`
	Type string      `json:"type"`
	FK   *ForeignKey `json:"fk,omitempty"`
}

type Table struct {
	PK      string   `json:"pk"`
	Columns []Column `json:"columns"`
}

type Schema struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Engine   string           `json:"engine"`
	Sample   bool             `json:"sample,omitempty"`
	ViewName string           `json:"viewName"`
	Tables   map[string]Table `json:"tables"`
}

var (
	createTableRE = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([^\s(]+)\s*\(`)
	columnRE      = regexp.MustCompile(`(?s)^(\x60[^\x60]+\x60|"[^"]+"|\[[^\]]+\]|[\w$]+)\s+([\s\S]*)$`)
	typeRE        = regexp.MustCompile("^[\\[\\]\"`\\w]+(?:\\s*\\([^)]*\\))?")
	foreignKeyRE  = regexp.MustCompile(`(?is)FOREIGN\s+KEY\s*\(\s*([^\s,)]+)\s*\)\s*REFERENCES\s+([^\s(]+)\s*\(\s*([^\s,)]+)`)
	referencesRE  = regexp.MustCompile(`(?is)\bREFERENCES\s+([^\s(]+)\s*\(\s*([^\s,)]+)`)
	primaryKeyRE  = regexp.MustCompile(`(?is)PRIMARY\s+KEY[^(]*\(\s*([^),\s]+)`)
	constraintRE  = regexp.MustCompile(`(?is)^(?:CONSTRAINT\s+\S+\s+)?(?:PRIMARY|FOREIGN|UNIQUE|KEY|INDEX|CHECK|FULLTEXT|SPATIAL|EXCLUDE)\b`)
	alterKeyRE    = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:ONLY\s+)?(\S+)\s+ADD\s+(?:CONSTRAINT\s+\S+\s+)?FOREIGN\s+KEY\s*\(\s*([^\s,)]+)\s*\)\s*REFERENCES\s+([^\s(]+)\s*\(\s*([^\s,)]+)`)
)

func ParseDDL(text string) (map[string]Table, error) {
	source := stripSQLComments(text)
	tables := make(map[string]Table)

	for _, match := range createTableRE.FindAllStringSubmatchIndex(source, -1) {
		name := cleanIdentifier(source[match[2]:match[3]])
		body, end, err := tableDefinition(source, match[1]-1)
		if err != nil {
			return nil, fmt.Errorf("ตาราง %s: %w", name, err)
		}

		columns := make([]Column, 0)
		foreignKeys := make(map[string]ForeignKey)
		primaryKey := ""
		for _, raw := range splitTopLevel(body) {
			part := strings.TrimSpace(raw)
			if part == "" {
				continue
			}
			if constraintRE.MatchString(part) {
				if foreign := foreignKeyRE.FindStringSubmatch(part); foreign != nil {
					foreignKeys[cleanIdentifier(foreign[1])] = ForeignKey{
						Table:  cleanIdentifier(foreign[2]),
						Column: cleanIdentifier(foreign[3]),
					}
				}
				if primary := primaryKeyRE.FindStringSubmatch(part); primary != nil {
					primaryKey = cleanIdentifier(primary[1])
				}
				continue
			}

			columnMatch := columnRE.FindStringSubmatch(part)
			if columnMatch == nil {
				continue
			}
			columnName := cleanIdentifier(columnMatch[1])
			rest := columnMatch[2]
			columnType := strings.Trim(typeRE.FindString(rest), "`\"[]")
			if columnName == "" || columnType == "" {
				continue
			}
			columns = append(columns, Column{Name: columnName, Type: columnType})
			if strings.Contains(strings.ToUpper(rest), "PRIMARY KEY") {
				primaryKey = columnName
			}
			if reference := referencesRE.FindStringSubmatch(rest); reference != nil {
				foreignKeys[columnName] = ForeignKey{
					Table:  cleanIdentifier(reference[1]),
					Column: cleanIdentifier(reference[2]),
				}
			}
		}
		for i := range columns {
			if foreignKey, ok := foreignKeys[columns[i].Name]; ok {
				columns[i].FK = &foreignKey
			}
		}
		if len(columns) == 0 {
			return nil, fmt.Errorf("ตาราง %s ไม่มีคอลัมน์ที่อ่านได้", name)
		}
		if primaryKey == "" {
			primaryKey = columns[0].Name
			for _, column := range columns {
				if column.Name == "id" {
					primaryKey = column.Name
					break
				}
			}
		}
		tables[name] = Table{PK: primaryKey, Columns: columns}
		if end <= match[1] {
			return nil, errors.New("ตำแหน่งคำสั่ง CREATE TABLE ไม่ถูกต้อง")
		}
	}

	if len(tables) == 0 {
		return nil, errors.New("ไม่พบคำสั่ง CREATE TABLE ที่อ่านได้")
	}
	for _, match := range alterKeyRE.FindAllStringSubmatch(source, -1) {
		table, ok := tables[cleanIdentifier(match[1])]
		if !ok {
			continue
		}
		columnName := cleanIdentifier(match[2])
		for i := range table.Columns {
			if table.Columns[i].Name == columnName {
				table.Columns[i].FK = &ForeignKey{
					Table:  cleanIdentifier(match[3]),
					Column: cleanIdentifier(match[4]),
				}
				break
			}
		}
		tables[cleanIdentifier(match[1])] = table
	}
	return tables, nil
}

func cleanIdentifier(value string) string {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ".")
	value = parts[len(parts)-1]
	return strings.Trim(value, "`\"[]")
}

func stripSQLComments(source string) string {
	var out strings.Builder
	for i := 0; i < len(source); {
		if source[i] == '\'' || source[i] == '"' || source[i] == '`' || source[i] == '[' {
			end := quotedEnd(source, i)
			out.WriteString(source[i:end])
			i = end
			continue
		}
		if strings.HasPrefix(source[i:], "--") {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}
		if strings.HasPrefix(source[i:], "/*") {
			if end := strings.Index(source[i+2:], "*/"); end >= 0 {
				i += end + 4
				continue
			}
			break
		}
		out.WriteByte(source[i])
		i++
	}
	return out.String()
}

func quotedEnd(source string, start int) int {
	opening := source[start]
	closing := opening
	if opening == '[' {
		closing = ']'
	}
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' && i+1 < len(source) {
			i++
			continue
		}
		if source[i] != closing {
			continue
		}
		if opening != '[' && i+1 < len(source) && source[i+1] == closing {
			i++
			continue
		}
		return i + 1
	}
	return len(source)
}

func tableDefinition(source string, opening int) (string, int, error) {
	if opening < 0 || opening >= len(source) || source[opening] != '(' {
		return "", 0, errors.New("ไม่พบวงเล็บเปิด")
	}
	depth := 1
	for i := opening + 1; i < len(source); i++ {
		if source[i] == '\'' || source[i] == '"' || source[i] == '`' || source[i] == '[' {
			i = quotedEnd(source, i) - 1
			continue
		}
		switch source[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return source[opening+1 : i], i + 1, nil
			}
		}
	}
	return "", 0, errors.New("วงเล็บไม่ครบ")
}

func splitTopLevel(source string) []string {
	parts := make([]string, 0)
	depth, start := 0, 0
	for i := 0; i < len(source); i++ {
		if source[i] == '\'' || source[i] == '"' || source[i] == '`' || source[i] == '[' {
			i = quotedEnd(source, i) - 1
			continue
		}
		switch source[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, source[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, source[start:])
}
