package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func TestAnalyzeTextReportsTheFigures(t *testing.T) {
	result := log.AnalyzeResult{
		Path:       "/logs/access.log",
		Bytes:      2048,
		Lines:      1200,
		Blank:      4,
		Type:       log.TypeAccess,
		Sampled:    200,
		DurationMs: 12,
	}

	got := render(t, logAnalyzeText(result))
	for _, want := range []string{"/logs/access.log", "2.0 KB", "1,200", "http-access", "12 ms"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestHTTPTextShowsEveryListing(t *testing.T) {
	result := log.HTTPResult{
		Path:     "/logs/access.log",
		Requests: 100,
		Unparsed: 2,
		Methods:  []log.Count{{Value: "GET", Count: 90, Percent: 90}},
		StatusClasses: []log.Count{
			{Value: "2xx", Count: 95, Percent: 95},
			{Value: "5xx", Count: 5, Percent: 5},
		},
		StatusCodes: []log.Count{{Value: "200", Count: 95, Percent: 95}},
		TopPaths:    []log.Count{{Value: "/api/users", Count: 40, Percent: 40}},
		TopClients:  []log.Count{{Value: "10.0.0.1", Count: 60, Percent: 60}},
	}

	got := render(t, logHTTPText(result))
	for _, want := range []string{
		"Methods", "Status classes", "Status codes", "Top endpoints", "Top clients",
		"GET", "/api/users", "10.0.0.1", "90.0%",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// Lines a parser could not read are stated, not hidden. A summary of ninety
// percent of a file has to say which ninety.
func TestHTTPTextStatesUnreadableLines(t *testing.T) {
	got := render(t, logHTTPText(log.HTTPResult{Requests: 100, Unparsed: 7}))
	if !strings.Contains(got, "7 unreadable") {
		t.Errorf("output = %q, want it to mention the unreadable lines", got)
	}

	clean := render(t, logHTTPText(log.HTTPResult{Requests: 100}))
	if strings.Contains(clean, "unreadable") {
		t.Errorf("output = %q, want no note when every line parsed", clean)
	}
}

func TestErrorsTextLeadsWithTheLineNumber(t *testing.T) {
	result := log.ErrorsResult{
		Path:       "/logs/app.log",
		Lines:      500,
		Errors:     11,
		Warnings:   3,
		Severities: []log.Count{{Value: log.SeverityError, Count: 10, Percent: 90.9}},
		Categories: []log.Count{{Value: log.CategoryConnection, Count: 3, Percent: 27.3}},
		Messages: []log.ErrorMessage{{
			Message:   "database connection failed",
			Severity:  log.SeverityError,
			Category:  log.CategoryConnection,
			Count:     3,
			Percent:   27.3,
			FirstLine: 5,
			LastLine:  7,
		}},
	}

	got := render(t, logErrorsText(result))
	for _, want := range []string{"By severity", "By category", "Most common messages",
		"database connection failed", "connection", "5"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestSearchTextSaysWhenTheListingWasCapped(t *testing.T) {
	result := log.SearchResult{
		Path:    "/logs/app.log",
		Query:   "timeout",
		Lines:   9000,
		Matches: 900,
		Limited: true,
		Results: []log.Match{{Line: 42, Text: "request timeout"}},
	}

	got := render(t, logSearchText(result))
	if !strings.Contains(got, "900") || !strings.Contains(got, "showing the first") {
		t.Errorf("output = %q, want the real count and the cap", got)
	}
	if !strings.Contains(got, "42") {
		t.Errorf("output = %q, want the line number", got)
	}
}

func TestStatsTextReportsTheExtremes(t *testing.T) {
	result := log.StatsResult{
		Path:              "/logs/app.log",
		Bytes:             4096,
		Lines:             120,
		Blank:             2,
		AverageLineBytes:  84.5,
		LongestLineBytes:  9000,
		ShortestLineBytes: 12,
		ShortestLine:      7,
		LongestLines:      []log.LineLength{{Line: 44, Bytes: 9000, Text: "payload"}},
	}

	got := render(t, logStatsText(result))
	for _, want := range []string{"84.5 bytes", "9,000 bytes", "line 7", "Longest lines", "44"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// The row view is what --output csv renders, so it has to carry the numbers
// unformatted: a spreadsheet given "1,204" has to be told it is a number.
func TestLogTablesAreMachineReadable(t *testing.T) {
	table := logTopTable(log.TopResult{
		Subject: "endpoint",
		Entries: []log.Count{{Value: "/api/users", Count: 1204, Percent: 40.5}},
	})()

	if len(table.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(table.Rows))
	}
	if table.Rows[0][1] != "1204" {
		t.Errorf("count cell = %q, want an unformatted number", table.Rows[0][1])
	}
	if table.Rows[0][2] != "40.5" {
		t.Errorf("percent cell = %q, want no percent sign", table.Rows[0][2])
	}
}

// The five HTTP listings become one set of rows tagged by section, so one CSV
// file holds the whole summary.
func TestHTTPTableFlattensEverySection(t *testing.T) {
	table := logHTTPTable(log.HTTPResult{
		Methods:       []log.Count{{Value: "GET", Count: 90}},
		StatusClasses: []log.Count{{Value: "2xx", Count: 95}},
		StatusCodes:   []log.Count{{Value: "200", Count: 95}},
		TopPaths:      []log.Count{{Value: "/", Count: 50}},
		TopClients:    []log.Count{{Value: "10.0.0.1", Count: 60}},
	})()

	if len(table.Rows) != 5 {
		t.Fatalf("rows = %d, want one per listing", len(table.Rows))
	}
	sections := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		sections = append(sections, row[0])
	}
	for _, want := range []string{"method", "class", "status", "endpoint", "client"} {
		if !contains(sections, want) {
			t.Errorf("sections = %v, want one named %q", sections, want)
		}
	}
}

// The csv renderer is what makes the row view worth having, and a result
// without one has to say so rather than emit an invented shape.
func TestCSVRendererWritesRowsAndRefusesWithout(t *testing.T) {
	renderer, err := output.NewRenderer("csv")
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	rows, ok := renderer.(output.RowRenderer)
	if !ok {
		t.Fatal("the csv renderer must accept a row view")
	}

	envelope := output.NewEnvelope(output.Meta{}, nil)
	var buffer bytes.Buffer

	table := logTopTable(log.TopResult{
		Subject: "endpoint",
		Entries: []log.Count{{Value: "/a,b", Count: 2, Percent: 50}},
	})
	if err := rows.RenderRows(&buffer, envelope, table); err != nil {
		t.Fatalf("RenderRows: %v", err)
	}

	got := buffer.String()
	if !strings.HasPrefix(got, "endpoint,count,percent") {
		t.Errorf("output = %q, want a header row", got)
	}
	// A value containing the delimiter has to survive it.
	if !strings.Contains(got, `"/a,b"`) {
		t.Errorf("output = %q, want the comma quoted", got)
	}

	buffer.Reset()
	if err := rows.RenderRows(&buffer, envelope, nil); err == nil {
		t.Error("a result with no row view must be refused, not invented")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestStatusTextShowsTheFamiliesAndTheFailureShare(t *testing.T) {
	result := log.StatusResult{
		Path:     "/logs/access.log",
		Requests: 24,
		Unparsed: 2,
		Errors:   8,
		Classes: []log.Count{
			{Value: "1xx", Count: 0, Percent: 0},
			{Value: "2xx", Count: 14, Percent: 58.3},
			{Value: "3xx", Count: 2, Percent: 8.3},
			{Value: "4xx", Count: 5, Percent: 20.8},
			{Value: "5xx", Count: 3, Percent: 12.5},
		},
		Codes: []log.Count{{Value: "200", Count: 12, Percent: 50}},
	}

	got := render(t, logStatusText(result))
	for _, want := range []string{"Status classes", "Most common codes", "1xx", "5xx", "33.3%"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestStatusTableCarriesBothListings(t *testing.T) {
	table := logStatusTable(log.StatusResult{
		Classes: []log.Count{{Value: "2xx", Count: 14, Percent: 58.3}},
		Codes:   []log.Count{{Value: "200", Count: 12, Percent: 50}},
	})()

	if len(table.Rows) != 2 {
		t.Fatalf("rows = %d, want one per listing", len(table.Rows))
	}
	if table.Rows[0][0] != "class" || table.Rows[1][0] != "code" {
		t.Errorf("sections = %q and %q, want class and code", table.Rows[0][0], table.Rows[1][0])
	}
}

func TestPercentOfHandlesAnEmptyFile(t *testing.T) {
	if got := logPercentOf(0, 0); got != 0 {
		t.Errorf("percentOf(0, 0) = %v, want 0 rather than a division by zero", got)
	}
	if got := logPercentOf(1, 4); got != 25 {
		t.Errorf("percentOf(1, 4) = %v, want 25", got)
	}
}

func TestTopTextNamesTheSubject(t *testing.T) {
	result := log.TopResult{
		Path:     "/logs/access.log",
		Subject:  "client",
		Requests: 24,
		Unique:   4,
		Entries:  []log.Count{{Value: "198.51.100.7", Count: 9, Percent: 37.5}},
	}

	got := render(t, logTopText(result))
	for _, want := range []string{"unique clients", "Most requested", "198.51.100.7", "37.5%"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// Every log result has a row view, and every one of them has to survive being
// built from an empty result: a file with nothing in it is an ordinary input.
func TestEveryLogTableHandlesAnEmptyResult(t *testing.T) {
	tables := map[string]output.TableFunc{
		"analyze": logAnalyzeTable(log.AnalyzeResult{Path: "empty.log", Type: log.TypeText}),
		"http":    logHTTPTable(log.HTTPResult{}),
		"status":  logStatusTable(log.StatusResult{}),
		"top":     logTopTable(log.TopResult{Subject: "endpoint"}),
		"errors":  logErrorsTable(log.ErrorsResult{}),
		"search":  logSearchTable(log.SearchResult{}),
		"stats":   logStatsTable(log.StatsResult{}),
	}

	for name, table := range tables {
		view := table()
		if len(view.Columns) == 0 {
			t.Errorf("%s: no columns, so a csv file would have no header", name)
		}
		if view.Rows == nil {
			continue
		}
		for index, row := range view.Rows {
			if len(row) != len(view.Columns) {
				t.Errorf("%s: row %d has %d cells, want %d",
					name, index, len(row), len(view.Columns))
			}
		}
	}
}

func TestErrorsAndStatsTablesCarryTheirListings(t *testing.T) {
	errorsTable := logErrorsTable(log.ErrorsResult{
		Messages: []log.ErrorMessage{{
			Message: "database connection failed", Severity: log.SeverityError,
			Category: log.CategoryConnection, Count: 3, Percent: 27.3,
			FirstLine: 5, LastLine: 7,
		}},
	})()
	if len(errorsTable.Rows) != 1 || errorsTable.Rows[0][0] != "3" {
		t.Fatalf("errors rows = %v, want one row counting three", errorsTable.Rows)
	}
	if errorsTable.Rows[0][4] != "5" || errorsTable.Rows[0][5] != "7" {
		t.Errorf("line numbers = %q and %q, want 5 and 7",
			errorsTable.Rows[0][4], errorsTable.Rows[0][5])
	}

	statsTable := logStatsTable(log.StatsResult{
		LongestLines: []log.LineLength{{Line: 44, Bytes: 9000, Text: "payload"}},
	})()
	if len(statsTable.Rows) != 1 || statsTable.Rows[0][1] != "9000" {
		t.Fatalf("stats rows = %v, want an unformatted byte count", statsTable.Rows)
	}

	searchTable := logSearchTable(log.SearchResult{
		Results: []log.Match{{Line: 42, Text: "request timeout"}},
	})()
	if len(searchTable.Rows) != 1 || searchTable.Rows[0][0] != "42" {
		t.Fatalf("search rows = %v, want the line number first", searchTable.Rows)
	}
}

// A log command has no sensible default file: unlike the file commands, "the
// current directory" is not a log.
func TestLogFileRequiresExactlyOneFile(t *testing.T) {
	if got, err := logFile([]string{"app.log"}); err != nil || got != "app.log" {
		t.Fatalf("logFile = %q, %v; want the file and no error", got, err)
	}
	for _, args := range [][]string{nil, {"a.log", "b.log"}} {
		if _, err := logFile(args); err == nil {
			t.Errorf("logFile(%v) was accepted", args)
		} else if errors.CodeOf(err) != errors.CodeInvalidInput {
			t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
		}
	}
}

// A file holding no access log entries is a warning, not a failure: the
// command worked, the file is not what the user thought.
func TestEmptyAccessLogWarnsRatherThanFails(t *testing.T) {
	env, _, stderr := newTestEnv(t, "table")

	warnEmptyAccess(env, 0, 40)
	if len(env.warnings) != 1 {
		t.Fatalf("warnings = %v, want one", env.warnings)
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty; the warning should also be logged")
	}

	warnEmptyAccess(env, 12, 40)
	warnEmptyAccess(env, 0, 0)
	if len(env.warnings) != 1 {
		t.Errorf("warnings = %v, want no warning when there were requests or no lines", env.warnings)
	}

	warnTruncatedRanking(env, false)
	if len(env.warnings) != 1 {
		t.Errorf("warnings = %v, want none added for a complete ranking", env.warnings)
	}
	warnTruncatedRanking(env, true)
	if len(env.warnings) != 2 {
		t.Errorf("warnings = %v, want the truncated ranking reported", env.warnings)
	}
}

// EmitTable renders rows when the renderer wants them and the text view
// otherwise, which is what makes one command work in every format.
func TestEmitTableFeedsWhicheverViewTheRendererNeeds(t *testing.T) {
	result := log.TopResult{
		Subject: "endpoint",
		Entries: []log.Count{{Value: "/api/users", Count: 4, Percent: 16.7}},
	}

	csvEnv, csvOut, _ := newTestEnv(t, "csv")
	if err := csvEnv.EmitTable(result, logTopText(result), logTopTable(result)); err != nil {
		t.Fatalf("EmitTable: %v", err)
	}
	if !strings.HasPrefix(csvOut.String(), "endpoint,count,percent") {
		t.Errorf("csv output = %q, want the row view", csvOut.String())
	}

	tableEnv, tableOut, _ := newTestEnv(t, "table")
	if err := tableEnv.EmitTable(result, logTopText(result), logTopTable(result)); err != nil {
		t.Fatalf("EmitTable: %v", err)
	}
	if !strings.Contains(tableOut.String(), "Most requested") {
		t.Errorf("table output = %q, want the text view", tableOut.String())
	}
}
