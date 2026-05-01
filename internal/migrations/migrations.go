package migrations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// DownMarker starts the optional down section inside a migration file.
	DownMarker = "-- litemigrate down"
	// timestampLayout is the expected format for the timestamp prefix in migration filenames.
	timestampLayout = "20060102150405"
)

var (
	// migrationNamePattern matches valid migration filenames and captures the version and description.
	migrationNamePattern = regexp.MustCompile(`^(\d{14})_([a-z0-9][a-z0-9_-]*)\.sql$`)
	// whitespacePattern matches sequences of whitespace characters for normalization.
	whitespacePattern = regexp.MustCompile(`\s+`)
)

// File contains a parsed migration file.
type File struct {
	// Version is the timestamp prefix extracted from the migration filename.
	Version string
	// Description is the slug portion extracted from the migration filename.
	Description string
	// Name is the base filename of the migration file.
	Name string
	// Path is the absolute or relative path used to load the migration file.
	Path string
	// Up contains the raw SQL source for the up section.
	Up string
	// Down contains the raw SQL source for the optional down section.
	Down string
	// UpStatements contains the executable up statements in order.
	UpStatements []string
	// DownStatements contains the executable down statements in order.
	DownStatements []string
}

// LoadDir reads and parses all migration files from dir.
func LoadDir(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migration directory %q: %w", dir, err)
	}

	loaded := make([]File, 0, len(entries))
	versions := make(map[string]string, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) != ".sql" {
			continue
		}

		path := filepath.Join(dir, name)
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read migration file %q: %w", path, readErr)
		}

		migration, parseErr := ParseFile(path, contents)
		if parseErr != nil {
			return nil, parseErr
		}

		if existing, ok := versions[migration.Version]; ok {
			return nil, fmt.Errorf(
				"duplicate migration version %q in %q and %q",
				migration.Version,
				existing,
				migration.Name,
			)
		}

		versions[migration.Version] = migration.Name
		loaded = append(loaded, migration)
	}

	slices.SortFunc(loaded, func(left, right File) int {
		return strings.Compare(left.Name, right.Name)
	})

	return loaded, nil
}

// NewFileName builds a UTC migration filename from a human description.
func NewFileName(now time.Time, description string) (string, error) {
	slug := slugify(description)
	if slug == "" {
		return "", errors.New("migration description must not be empty")
	}

	return fmt.Sprintf("%s_%s.sql", now.UTC().Format(timestampLayout), slug), nil
}

// ParseFile parses a migration file from its path and contents.
func ParseFile(path string, contents []byte) (File, error) {
	name := filepath.Base(path)
	matches := migrationNamePattern.FindStringSubmatch(name)
	if matches == nil {
		return File{}, fmt.Errorf("invalid migration filename %q", name)
	}

	upSQL, downSQL, err := splitSections(string(contents))
	if err != nil {
		return File{}, fmt.Errorf("parse migration file %q: %w", name, err)
	}

	upStatements, err := SplitStatements(upSQL)
	if err != nil {
		return File{}, fmt.Errorf("parse up statements for %q: %w", name, err)
	}
	if len(upStatements) == 0 {
		return File{}, fmt.Errorf("migration %q must contain at least one up statement", name)
	}

	downStatements, err := SplitStatements(downSQL)
	if err != nil {
		return File{}, fmt.Errorf("parse down statements for %q: %w", name, err)
	}

	return File{
		Version:        matches[1],
		Description:    matches[2],
		Name:           name,
		Path:           path,
		Up:             upSQL,
		Down:           downSQL,
		UpStatements:   upStatements,
		DownStatements: downStatements,
	}, nil
}

// SplitStatements splits SQL text into executable SQLite statements.
func SplitStatements(sqlText string) ([]string, error) {
	type parserState struct {
		firstTokens    []string
		inTrigger      bool
		seenBegin      bool
		beginDepth     int
		caseDepth      int
		inSingleQuote  bool
		inDoubleQuote  bool
		inBacktick     bool
		inBracket      bool
		inLineComment  bool
		inBlockComment bool
	}

	var (
		statements []string
		current    strings.Builder
		token      strings.Builder
		state      parserState
	)

	resetStatement := func() {
		current.Reset()
		token.Reset()
		state = parserState{}
	}

	normalizeStatement := func(statement string) string {
		trimmed := strings.TrimSpace(statement)
		trimmed = strings.TrimSuffix(trimmed, ";")
		return strings.TrimSpace(trimmed)
	}

	flushStatement := func() {
		statement := normalizeStatement(current.String())
		if statement != "" && containsExecutableSQL(statement) {
			statements = append(statements, statement)
		}
		resetStatement()
	}

	flushToken := func() {
		if token.Len() == 0 {
			return
		}

		upper := strings.ToUpper(token.String())
		token.Reset()

		if len(state.firstTokens) < 8 {
			state.firstTokens = append(state.firstTokens, upper)
		}

		if !state.inTrigger {
			if len(state.firstTokens) >= 1 && state.firstTokens[0] == "CREATE" &&
				upper == "TRIGGER" {
				state.inTrigger = true
			}
			return
		}

		switch upper {
		case "BEGIN":
			state.seenBegin = true
			state.beginDepth++
		case "CASE":
			if state.beginDepth > 0 {
				state.caseDepth++
			}
		case "END":
			if state.beginDepth == 0 {
				return
			}
			if state.caseDepth > 0 {
				state.caseDepth--
				return
			}
			state.beginDepth--
		}
	}

	for index := range len(sqlText) {
		char := sqlText[index]
		next := byte(0)
		if index+1 < len(sqlText) {
			next = sqlText[index+1]
		}

		switch {
		case state.inLineComment:
			current.WriteByte(char)
			if char == '\n' {
				state.inLineComment = false
			}
			continue
		case state.inBlockComment:
			current.WriteByte(char)
			if char == '*' && next == '/' {
				current.WriteByte(next)
				state.inBlockComment = false
				continue
			}
			continue
		case state.inSingleQuote:
			current.WriteByte(char)
			if char == '\'' {
				if next == '\'' {
					current.WriteByte(next)
					continue
				}
				state.inSingleQuote = false
			}
			continue
		case state.inDoubleQuote:
			current.WriteByte(char)
			if char == '"' {
				if next == '"' {
					current.WriteByte(next)
					continue
				}
				state.inDoubleQuote = false
			}
			continue
		case state.inBacktick:
			current.WriteByte(char)
			if char == '`' {
				if next == '`' {
					current.WriteByte(next)
					continue
				}
				state.inBacktick = false
			}
			continue
		case state.inBracket:
			current.WriteByte(char)
			if char == ']' {
				state.inBracket = false
			}
			continue
		}

		if char == '-' && next == '-' {
			flushToken()
			current.WriteString("--")
			state.inLineComment = true
			continue
		}

		if char == '/' && next == '*' {
			flushToken()
			current.WriteString("/*")
			state.inBlockComment = true
			continue
		}

		switch char {
		case '\'':
			flushToken()
			current.WriteByte(char)
			state.inSingleQuote = true
			continue
		case '"':
			flushToken()
			current.WriteByte(char)
			state.inDoubleQuote = true
			continue
		case '`':
			flushToken()
			current.WriteByte(char)
			state.inBacktick = true
			continue
		case '[':
			flushToken()
			current.WriteByte(char)
			state.inBracket = true
			continue
		}

		if isIdentifierChar(char) {
			current.WriteByte(char)
			token.WriteByte(char)
			continue
		}

		flushToken()

		if char == ';' {
			current.WriteByte(char)
			if state.inTrigger {
				if state.seenBegin && state.beginDepth == 0 {
					flushStatement()
				}
				continue
			}
			flushStatement()
			continue
		}

		current.WriteByte(char)
	}

	flushToken()

	if state.inSingleQuote || state.inDoubleQuote || state.inBacktick || state.inBracket ||
		state.inBlockComment {
		return nil, errors.New("unterminated SQL literal or comment")
	}
	if state.inTrigger && state.beginDepth > 0 {
		return nil, errors.New("unterminated trigger body")
	}

	if strings.TrimSpace(current.String()) != "" {
		flushStatement()
	}

	return statements, nil
}

func splitSections(contents string) (string, string, error) {
	normalized := strings.ReplaceAll(contents, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	downIndex := -1
	for index, line := range lines {
		if whitespacePattern.ReplaceAllString(
			strings.TrimSpace(line),
			"",
		) != whitespacePattern.ReplaceAllString(
			DownMarker,
			"",
		) {
			continue
		}
		if downIndex >= 0 {
			return "", "", errors.New("down marker appears more than once")
		}
		downIndex = index
	}

	if downIndex < 0 {
		return strings.TrimSpace(normalized), "", nil
	}

	up := strings.TrimSpace(strings.Join(lines[:downIndex], "\n"))
	down := strings.TrimSpace(strings.Join(lines[downIndex+1:], "\n"))

	return up, down, nil
}

func slugify(description string) string {
	description = strings.ToLower(strings.TrimSpace(description))
	if description == "" {
		return ""
	}

	var builder strings.Builder
	lastUnderscore := false

	for _, char := range description {
		isLetter := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'

		if isLetter || isDigit {
			builder.WriteRune(char)
			lastUnderscore = false
			continue
		}

		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(builder.String(), "_")
}

func containsExecutableSQL(statement string) bool {
	var (
		inLineComment  bool
		inBlockComment bool
		inSingleQuote  bool
		inDoubleQuote  bool
	)

	for index := range len(statement) {
		char := statement[index]
		next := byte(0)
		if index+1 < len(statement) {
			next = statement[index+1]
		}

		switch {
		case inLineComment:
			if char == '\n' {
				inLineComment = false
			}
			continue
		case inBlockComment:
			if char == '*' && next == '/' {
				inBlockComment = false
			}
			continue
		case inSingleQuote:
			if char == '\'' && next != '\'' {
				inSingleQuote = false
			}
			continue
		case inDoubleQuote:
			if char == '"' && next != '"' {
				inDoubleQuote = false
			}
			continue
		}

		if char == '-' && next == '-' {
			inLineComment = true
			continue
		}
		if char == '/' && next == '*' {
			inBlockComment = true
			continue
		}
		if char == '\'' {
			return true
		}
		if char == '"' {
			return true
		}
		if !isWhitespace(char) {
			return true
		}
	}

	return false
}

func isIdentifierChar(char byte) bool {
	return char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}

func isWhitespace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r'
}
