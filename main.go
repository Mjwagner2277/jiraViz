package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const appName = "JiraViz"

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

type Issue struct {
	Key         string
	Project     string
	Summary     string
	IssueType   string
	Status      string
	Assignee    string
	FixVersions []string
	EpicKey     string
	ParentKey   string
	Start       time.Time
	Due         time.Time
	StoryPoints float64
	Planned     bool
	DependsOn   []string
	Blocks      []string
}

type FixVersionMetric struct {
	Name         string
	Total        int
	Done         int
	Percent      int
	Start        time.Time
	Due          time.Time
	DurationDays int
	StoryPoints  float64
	DonePoints   float64
	Open         int
	Blocked      int
	StatusCounts []NameCount
}

type EpicMetric struct {
	Key          string
	Summary      string
	Status       string
	IssueCount   int
	DoneCount    int
	Percent      int
	Start        time.Time
	Due          time.Time
	DurationDays int
	StoryPoints  float64
	DonePoints   float64
	DependsOn    []string
	Blocks       []string
	OpenIssues   []Issue
}

type NameCount struct {
	Name  string
	Count int
}

type CriticalPath struct {
	Epics       []EpicMetric
	TotalDays   int
	TotalPoints float64
}

type Report struct {
	ProjectName        string
	GeneratedAt        string
	RangeStart         string
	RangeEnd           string
	IssueCount         int
	PlannedIssueCount  int
	FixVersions        []FixVersionMetric
	Epics              []EpicMetric
	CriticalPaths      []CriticalPath
	TopCriticalPath    CriticalPath
	MaxVersionTotal    int
	MaxEpicDuration    int
	NextSixMonthsLabel string
}

func main() {
	input := flag.String("input", "", "Jira CSV input path")
	output := flag.String("out", "jiraviz-report.svg", "output path")
	project := flag.String("project", "", "project key or name to visualize")
	listProjects := flag.Bool("list-projects", false, "list projects discovered in the CSV and exit")
	mode := flag.String("mode", "auto", "largest epic ranking mode: auto, points, or issues")
	asOf := flag.String("as-of", "", "report date in YYYY-MM-DD format; defaults to today")
	sample := flag.Bool("sample", false, "write a sample CSV to the input path before rendering")
	showVersion := flag.Bool("version", false, "print version and build metadata")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s commit=%s built=%s\n", appName, version, commit, buildDate)
		return
	}

	if *input == "" {
		fmt.Fprintf(os.Stderr, "usage: jiraviz -input issues.csv [-project PROJECT] [-out report.svg] [-list-projects] [-sample]\n")
		os.Exit(2)
	}

	if *sample {
		if err := writeSampleCSV(*input); err != nil {
			fatal(err)
		}
	}

	issues, err := readIssues(*input)
	if err != nil {
		fatal(err)
	}

	projects := projectCounts(issues)
	if *listProjects {
		printProjects(projects)
		return
	}

	selectedProject := strings.TrimSpace(*project)
	if selectedProject == "" && len(projects) == 1 {
		for name := range projects {
			selectedProject = name
		}
	}
	if selectedProject == "" && len(projects) > 1 {
		printProjects(projects)
		fatal(errors.New("multiple projects found; choose one with -project"))
	}
	if selectedProject != "" {
		var filterErr error
		issues, filterErr = filterIssuesByProject(issues, selectedProject)
		if filterErr != nil {
			printProjects(projects)
			fatal(filterErr)
		}
	}

	now := time.Now()
	if strings.TrimSpace(*asOf) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*asOf))
		if err != nil {
			fatal(fmt.Errorf("invalid -as-of date %q; use YYYY-MM-DD", *asOf))
		}
		now = parsed
	}

	report := buildReport(issues, now)
	data, err := renderSVG(report, *mode, now)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, data, 0644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\n", *output)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "jiraviz: %v\n", err)
	os.Exit(1)
}

func readIssues(path string) ([]Issue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("csv is empty")
	}

	header := make(map[string]int)
	for i, h := range rows[0] {
		header[normalizeHeader(h)] = i
	}

	var issues []Issue
	for _, row := range rows[1:] {
		key := cell(row, header, "key", "issuekey", "issue_key")
		if key == "" {
			continue
		}
		issue := Issue{
			Key:         key,
			Project:     firstNonEmpty(cell(row, header, "project", "projectkey", "project_key", "projectname", "project_name"), deriveProject(key)),
			Summary:     cell(row, header, "summary", "title"),
			IssueType:   cell(row, header, "issuetype", "issue_type", "type"),
			Status:      cell(row, header, "status"),
			Assignee:    cell(row, header, "assignee"),
			FixVersions: splitList(cell(row, header, "fixversions", "fixversion", "fix_version_s", "fix_version", "targetversion", "target_version")),
			EpicKey:     cell(row, header, "epiclink", "epic_link", "epickey", "epic_key", "epic"),
			ParentKey:   cell(row, header, "parent", "parentkey", "parent_key"),
			Start:       parseDate(cell(row, header, "startdate", "start_date", "start")),
			Due:         parseDate(cell(row, header, "duedate", "due_date", "due", "enddate", "end_date", "targetend")),
			StoryPoints: parseFloat(cell(row, header, "storypoints", "story_points", "points", "estimate")),
			Planned:     parsePlanned(cell(row, header, "planned", "committed", "inplan", "in_plan")),
			DependsOn:   splitList(cell(row, header, "dependson", "depends_on", "dependency", "dependencies", "blockedby", "blocked_by")),
			Blocks:      splitList(cell(row, header, "blocks", "blocking")),
		}
		if len(issue.FixVersions) == 0 {
			issue.FixVersions = []string{"Unassigned"}
		}
		if issue.IssueType == "" {
			issue.IssueType = "Task"
		}
		if issue.Planned == false && !hasAnyHeader(header, "planned", "committed", "inplan", "in_plan") {
			issue.Planned = true
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

func normalizeHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func cell(row []string, header map[string]int, names ...string) string {
	for _, name := range names {
		if i, ok := header[normalizeHeader(name)]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
	}
	return ""
}

func hasAnyHeader(header map[string]int, names ...string) bool {
	for _, name := range names {
		if _, ok := header[normalizeHeader(name)]; ok {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func deriveProject(key string) string {
	key = strings.TrimSpace(key)
	if i := strings.Index(key, "-"); i > 0 {
		return key[:i]
	}
	return key
}

func projectCounts(issues []Issue) map[string]int {
	counts := map[string]int{}
	for _, issue := range issues {
		project := firstNonEmpty(issue.Project, deriveProject(issue.Key), "Unknown")
		counts[project]++
	}
	return counts
}

func printProjects(projects map[string]int) {
	names := keysFromCounts(projects)
	fmt.Println("Projects:")
	for _, name := range names {
		fmt.Printf("- %s (%d issues)\n", name, projects[name])
	}
}

func keysFromCounts(counts map[string]int) []string {
	var names []string
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func filterIssuesByProject(issues []Issue, project string) ([]Issue, error) {
	var out []Issue
	for _, issue := range issues {
		if strings.EqualFold(issue.Project, project) || strings.EqualFold(deriveProject(issue.Key), project) {
			out = append(out, issue)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("project %q was not found in the CSV", project)
	}
	return out, nil
}

func splitList(s string) []string {
	var values []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == '\n'
	}) {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	layouts := []string{"2006-01-02", "1/2/2006", "01/02/2006", "2006/01/02", time.RFC3339}
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parsePlanned(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "yes", "y", "true", "1", "planned", "committed":
		return true
	default:
		return false
	}
}

func buildReport(issues []Issue, now time.Time) Report {
	versionMetrics := buildFixVersions(issues)
	epics := buildEpics(issues, now)
	paths := criticalPaths(epics)
	maxVersionTotal := 1
	for _, v := range versionMetrics {
		if v.Total > maxVersionTotal {
			maxVersionTotal = v.Total
		}
	}
	maxEpicDuration := 1
	for _, e := range epics {
		if e.DurationDays > maxEpicDuration {
			maxEpicDuration = e.DurationDays
		}
	}
	planned := 0
	for _, issue := range issues {
		if issue.Planned {
			planned++
		}
	}
	top := CriticalPath{}
	if len(paths) > 0 {
		top = paths[0]
	}
	rangeEnd := now.AddDate(0, 6, 0)
	return Report{
		ProjectName:        reportProjectName(issues),
		GeneratedAt:        now.Format("Jan 2, 2006 3:04 PM"),
		RangeStart:         now.Format("Jan 2, 2006"),
		RangeEnd:           rangeEnd.Format("Jan 2, 2006"),
		IssueCount:         len(issues),
		PlannedIssueCount:  planned,
		FixVersions:        versionMetrics,
		Epics:              epics,
		CriticalPaths:      paths,
		TopCriticalPath:    top,
		MaxVersionTotal:    maxVersionTotal,
		MaxEpicDuration:    maxEpicDuration,
		NextSixMonthsLabel: fmt.Sprintf("%s to %s", now.Format("Jan 2006"), rangeEnd.Format("Jan 2006")),
	}
}

func reportProjectName(issues []Issue) string {
	if len(issues) == 0 {
		return "Jira Project"
	}
	first := firstNonEmpty(issues[0].Project, deriveProject(issues[0].Key), "Jira Project")
	for _, issue := range issues[1:] {
		name := firstNonEmpty(issue.Project, deriveProject(issue.Key), "Jira Project")
		if !strings.EqualFold(first, name) {
			return "Jira Project Portfolio"
		}
	}
	return first
}

func buildFixVersions(issues []Issue) []FixVersionMetric {
	byVersion := map[string][]Issue{}
	for _, issue := range issues {
		if !issue.Planned {
			continue
		}
		for _, version := range issue.FixVersions {
			byVersion[version] = append(byVersion[version], issue)
		}
	}
	var metrics []FixVersionMetric
	for version, versionIssues := range byVersion {
		statuses := map[string]int{}
		m := FixVersionMetric{Name: version}
		for _, issue := range versionIssues {
			if !issue.Start.IsZero() && (m.Start.IsZero() || issue.Start.Before(m.Start)) {
				m.Start = issue.Start
			}
			if issue.Due.After(m.Due) {
				m.Due = issue.Due
			}
			if isEpicIssue(issue) {
				continue
			}
			m.Total++
			m.StoryPoints += issue.StoryPoints
			statuses[issue.Status]++
			if isDone(issue.Status) {
				m.Done++
				m.DonePoints += issue.StoryPoints
			} else {
				m.Open++
			}
			if len(issue.DependsOn) > 0 {
				m.Blocked++
			}
		}
		if m.Total > 0 {
			m.Percent = int(math.Round(float64(m.Done) / float64(m.Total) * 100))
		}
		if m.Start.IsZero() {
			m.Start = m.Due
		}
		if m.Due.IsZero() {
			m.Due = m.Start
		}
		if !m.Start.IsZero() && !m.Due.IsZero() {
			if m.Due.Before(m.Start) {
				m.Due = m.Start
			}
			m.DurationDays = max(1, int(m.Due.Sub(m.Start).Hours()/24)+1)
		}
		m.StatusCounts = sortedCounts(statuses)
		metrics = append(metrics, m)
	}
	sort.Slice(metrics, func(i, j int) bool {
		return naturalVersionLess(metrics[i].Name, metrics[j].Name)
	})
	return metrics
}

func buildEpics(issues []Issue, now time.Time) []EpicMetric {
	byKey := map[string]Issue{}
	for _, issue := range issues {
		byKey[issue.Key] = issue
	}
	children := map[string][]Issue{}
	epicLabels := map[string]string{}
	for _, issue := range issues {
		if strings.EqualFold(issue.IssueType, "epic") {
			epicLabels[issue.Key] = issue.Summary
		}
		epic := issue.EpicKey
		if epic == "" && issue.ParentKey != "" {
			epic = issue.ParentKey
		}
		if epic == "" && strings.EqualFold(issue.IssueType, "epic") {
			epic = issue.Key
		}
		if epic == "" {
			epic = "No Epic"
		}
		children[epic] = append(children[epic], issue)
		if epicLabels[epic] == "" {
			epicLabels[epic] = epic
		}
	}

	issueEpic := map[string]string{}
	for epic, list := range children {
		for _, issue := range list {
			issueEpic[issue.Key] = epic
		}
	}

	cutoff := now.AddDate(0, 6, 0)
	var epics []EpicMetric
	for epic, list := range children {
		m := EpicMetric{Key: epic, Summary: epicLabels[epic]}
		depends := map[string]bool{}
		blocks := map[string]bool{}
		for _, issue := range list {
			if !inWindow(issue, now, cutoff) {
				continue
			}
			if !issue.Start.IsZero() && (m.Start.IsZero() || issue.Start.Before(m.Start)) {
				m.Start = issue.Start
			}
			if issue.Due.After(m.Due) {
				m.Due = issue.Due
			}
			for _, dep := range issue.DependsOn {
				if depEpic := issueEpic[dep]; depEpic != "" && depEpic != epic {
					depends[depEpic] = true
				}
			}
			for _, block := range issue.Blocks {
				if blockEpic := issueEpic[block]; blockEpic != "" && blockEpic != epic {
					blocks[blockEpic] = true
				}
			}
			if isEpicIssue(issue) && issue.Key == epic {
				continue
			}
			m.IssueCount++
			m.StoryPoints += issue.StoryPoints
			if isDone(issue.Status) {
				m.DoneCount++
				m.DonePoints += issue.StoryPoints
			} else {
				m.OpenIssues = append(m.OpenIssues, issue)
			}
		}
		if m.IssueCount == 0 {
			continue
		}
		if m.Start.IsZero() {
			m.Start = now
		}
		if m.Due.IsZero() || m.Due.Before(m.Start) {
			m.Due = m.Start.AddDate(0, 0, max(7, m.IssueCount*3))
		}
		m.DurationDays = max(1, int(m.Due.Sub(m.Start).Hours()/24)+1)
		m.Percent = int(math.Round(float64(m.DoneCount) / float64(m.IssueCount) * 100))
		m.DependsOn = keys(depends)
		m.Blocks = keys(blocks)
		epics = append(epics, m)
	}
	sort.Slice(epics, func(i, j int) bool {
		if epics[i].Start.Equal(epics[j].Start) {
			return epics[i].Key < epics[j].Key
		}
		return epics[i].Start.Before(epics[j].Start)
	})
	return epics
}

func inWindow(issue Issue, start, end time.Time) bool {
	if issue.Start.IsZero() && issue.Due.IsZero() {
		return true
	}
	if !issue.Due.IsZero() && issue.Due.Before(start) {
		return false
	}
	if !issue.Start.IsZero() && issue.Start.After(end) {
		return false
	}
	return true
}

func criticalPaths(epics []EpicMetric) []CriticalPath {
	byKey := map[string]EpicMetric{}
	next := map[string][]string{}
	for _, epic := range epics {
		byKey[epic.Key] = epic
	}
	for _, epic := range epics {
		for _, dep := range epic.DependsOn {
			if _, ok := byKey[dep]; ok {
				next[dep] = append(next[dep], epic.Key)
			}
		}
		for _, block := range epic.Blocks {
			if _, ok := byKey[block]; ok {
				next[epic.Key] = append(next[epic.Key], block)
			}
		}
	}

	var paths []CriticalPath
	var walk func(key string, seen map[string]bool, path []EpicMetric)
	walk = func(key string, seen map[string]bool, path []EpicMetric) {
		if seen[key] {
			paths = append(paths, makePath(path))
			return
		}
		epic, ok := byKey[key]
		if !ok {
			return
		}
		seen[key] = true
		path = append(path, epic)
		if len(next[key]) == 0 {
			paths = append(paths, makePath(path))
			return
		}
		for _, child := range next[key] {
			walk(child, cloneSeen(seen), path)
		}
	}
	for _, epic := range epics {
		walk(epic.Key, map[string]bool{}, nil)
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].TotalDays == paths[j].TotalDays {
			return len(paths[i].Epics) > len(paths[j].Epics)
		}
		return paths[i].TotalDays > paths[j].TotalDays
	})
	if len(paths) > 5 {
		paths = paths[:5]
	}
	return paths
}

func makePath(epics []EpicMetric) CriticalPath {
	p := CriticalPath{Epics: append([]EpicMetric(nil), epics...)}
	for _, epic := range epics {
		p.TotalDays += epic.DurationDays
		p.TotalPoints += epic.StoryPoints
	}
	return p
}

func cloneSeen(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isDone(status string) bool {
	switch normalizeHeader(status) {
	case "done", "closed", "resolved", "complete", "completed", "released", "accepted":
		return true
	default:
		return false
	}
}

func isEpicIssue(issue Issue) bool {
	return strings.EqualFold(issue.IssueType, "epic")
}

func sortedCounts(counts map[string]int) []NameCount {
	var out []NameCount
	for k, v := range counts {
		if k == "" {
			k = "No Status"
		}
		out = append(out, NameCount{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Name < out[j].Name
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func keys(values map[string]bool) []string {
	var out []string
	for k := range values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func naturalVersionLess(a, b string) bool {
	return strings.ToLower(a) < strings.ToLower(b)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func renderHTML(report Report) ([]byte, error) {
	funcs := template.FuncMap{
		"pct": func(done, total int) int {
			if total == 0 {
				return 0
			}
			return int(math.Round(float64(done) / float64(total) * 100))
		},
		"barWidth": func(value, max int) int {
			if max == 0 {
				return 0
			}
			return maxInt(4, int(math.Round(float64(value)/float64(max)*100)))
		},
		"daysWidth": func(value, max int) int {
			if max == 0 {
				return 0
			}
			return maxInt(8, int(math.Round(float64(value)/float64(max)*100)))
		},
		"date": func(t time.Time) string {
			if t.IsZero() {
				return "Unscheduled"
			}
			return t.Format("Jan 2")
		},
		"joinEpics": func(epics []EpicMetric) string {
			var keys []string
			for _, epic := range epics {
				keys = append(keys, epic.Key)
			}
			return strings.Join(keys, " -> ")
		},
		"add": func(a, b int) int {
			return a + b
		},
	}
	tmpl, err := template.New("report").Funcs(funcs).Parse(reportTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, report); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type SVGReport struct {
	ProjectName  string
	GeneratedAt  string
	WindowLabel  string
	RankingLabel string
	Width        int
	Height       int
	ChartLeft    int
	ChartRight   int
	ChartWidth   int
	MonthTicks   []SVGTick
	TodayX       int
	Releases     []SVGRelease
	TopEpics     []SVGEpic
}

type SVGTick struct {
	Label string
	X     int
}

type SVGRelease struct {
	Name      string
	DateRange string
	Percent   int
	Total     int
	Done      int
	Color     string
	X         int
	Width     int
	FillWidth int
	DotX      int
	Y         int
}

type SVGEpic struct {
	Rank          int
	Key           string
	Name          string
	DateRange     string
	Percent       int
	IssueCount    int
	StoryPoints   string
	MetricLabel   string
	Color         string
	X             int
	Width         int
	FillWidth     int
	Y             int
	NotStarted    bool
	ContinuesFrom bool
	ContinuesPast bool
}

func renderSVG(report Report, mode string, now time.Time) ([]byte, error) {
	view := buildSVGReport(report, mode, now)
	tmpl, err := template.New("svg").Parse(svgTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildSVGReport(report Report, mode string, now time.Time) SVGReport {
	windowStart := dateOnly(now)
	windowEnd := windowStart.AddDate(0, 6, 0)
	left := 250
	right := 1120
	width := right - left
	colors := []string{"#2F6FED", "#188A7D", "#8A5FBF", "#C76B2C", "#58733B", "#B5485A", "#3D6B83", "#7A6A2B"}

	releases := make([]SVGRelease, 0, len(report.FixVersions))
	for i, version := range report.FixVersions {
		start := version.Start
		due := version.Due
		if start.IsZero() {
			start = windowStart.AddDate(0, i, 0)
		}
		if due.IsZero() || due.Before(start) {
			due = start.AddDate(0, 0, 21)
		}
		x1 := dateX(start, windowStart, windowEnd, left, width)
		x2 := dateX(due, windowStart, windowEnd, left, width)
		barWidth := maxInt(28, x2-x1)
		releases = append(releases, SVGRelease{
			Name:      version.Name,
			DateRange: dateRange(start, due),
			Percent:   version.Percent,
			Total:     version.Total,
			Done:      version.Done,
			Color:     colors[i%len(colors)],
			X:         x1,
			Width:     barWidth,
			FillWidth: maxInt(2, int(math.Round(float64(barWidth)*float64(version.Percent)/100))),
			DotX:      x1 + barWidth,
			Y:         218 + i*62,
		})
	}

	ranking, rankingLabel := rankedEpics(report.Epics, mode)
	topEpics := make([]SVGEpic, 0, len(ranking))
	for i, epic := range ranking {
		topEpics = append(topEpics, svgEpic(epic, i+1, 616+i*52, colors[i%len(colors)], windowStart, windowEnd, 650, 390, rankingLabel))
	}

	return SVGReport{
		ProjectName:  report.ProjectName,
		GeneratedAt:  report.GeneratedAt,
		WindowLabel:  fmt.Sprintf("%s to %s", windowStart.Format("Jan 2, 2006"), windowEnd.Format("Jan 2, 2006")),
		RankingLabel: rankingLabel,
		Width:        1200,
		Height:       820,
		ChartLeft:    left,
		ChartRight:   right,
		ChartWidth:   width,
		MonthTicks:   monthTicks(windowStart, windowEnd, left, width),
		TodayX:       dateX(windowStart, windowStart, windowEnd, left, width),
		Releases:     releases,
		TopEpics:     topEpics,
	}
}

func rankedEpics(epics []EpicMetric, mode string) ([]EpicMetric, string) {
	out := append([]EpicMetric(nil), epics...)
	usePoints := false
	for _, epic := range out {
		if epic.StoryPoints > 0 {
			usePoints = true
			break
		}
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "points":
		usePoints = true
	case "issues", "kanban":
		usePoints = false
	}
	sort.Slice(out, func(i, j int) bool {
		if usePoints && out[i].StoryPoints != out[j].StoryPoints {
			return out[i].StoryPoints > out[j].StoryPoints
		}
		if out[i].IssueCount != out[j].IssueCount {
			return out[i].IssueCount > out[j].IssueCount
		}
		return out[i].Start.Before(out[j].Start)
	})
	if len(out) > 3 {
		out = out[:3]
	}
	if usePoints {
		return out, "Ranked by story points"
	}
	return out, "Ranked by planned issue count"
}

func svgEpic(epic EpicMetric, rank, y int, color string, windowStart, windowEnd time.Time, left, width int, rankingLabel string) SVGEpic {
	start := epic.Start
	due := epic.Due
	if start.IsZero() {
		start = windowStart
	}
	if due.IsZero() || due.Before(start) {
		due = start.AddDate(0, 0, max(7, epic.IssueCount*3))
	}
	x1 := dateX(start, windowStart, windowEnd, left, width)
	x2 := dateX(due, windowStart, windowEnd, left, width)
	barWidth := maxInt(26, x2-x1)
	metric := fmt.Sprintf("%d planned issues", epic.IssueCount)
	if strings.Contains(strings.ToLower(rankingLabel), "story") {
		metric = fmt.Sprintf("%.0f pts · %d issues", epic.StoryPoints, epic.IssueCount)
	}
	return SVGEpic{
		Rank:          rank,
		Key:           epic.Key,
		Name:          epic.Summary,
		DateRange:     dateRange(start, due),
		Percent:       epic.Percent,
		IssueCount:    epic.IssueCount,
		StoryPoints:   fmt.Sprintf("%.0f", epic.StoryPoints),
		MetricLabel:   metric,
		Color:         color,
		X:             x1,
		Width:         barWidth,
		FillWidth:     maxInt(2, int(math.Round(float64(barWidth)*float64(epic.Percent)/100))),
		Y:             y,
		NotStarted:    epic.Percent == 0,
		ContinuesFrom: start.Before(windowStart),
		ContinuesPast: due.After(windowEnd),
	}
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func dateX(t, start, end time.Time, left, width int) int {
	if t.Before(start) {
		return left
	}
	if t.After(end) {
		return left + width
	}
	total := end.Sub(start).Hours()
	if total <= 0 {
		return left
	}
	return left + int(math.Round(t.Sub(start).Hours()/total*float64(width)))
}

func monthTicks(start, end time.Time, left, width int) []SVGTick {
	var ticks []SVGTick
	t := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	if t.Before(start) {
		t = t.AddDate(0, 1, 0)
	}
	for !t.After(end) {
		ticks = append(ticks, SVGTick{Label: t.Format("Jan"), X: dateX(t, start, end, left, width)})
		t = t.AddDate(0, 1, 0)
	}
	return ticks
}

func dateRange(start, due time.Time) string {
	if start.IsZero() && due.IsZero() {
		return "Unscheduled"
	}
	if due.IsZero() {
		return start.Format("Jan 2")
	}
	if start.IsZero() {
		return due.Format("Jan 2")
	}
	return fmt.Sprintf("%s - %s", start.Format("Jan 2"), due.Format("Jan 2"))
}

func writeSampleCSV(path string) error {
	return os.WriteFile(path, []byte(sampleCSV), 0644)
}

const svgTemplate = `<svg xmlns="http://www.w3.org/2000/svg" width="{{.Width}}" height="{{.Height}}" viewBox="0 0 {{.Width}} {{.Height}}" role="img" aria-labelledby="title desc">
  <title id="title">{{.ProjectName}} Program Portfolio Report</title>
  <desc id="desc">Jira CSV visualization with release timeline and important epics over the next six months.</desc>
  <defs>
    <style>
      .page { fill: #f6f7f9; }
      .panel { fill: #ffffff; stroke: #d8dde6; stroke-width: 1; }
      .ink { fill: #202124; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
      .muted { fill: #667085; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
      .small { font-size: 12px; }
      .label { font-size: 14px; font-weight: 700; }
      .title { font-size: 30px; font-weight: 800; letter-spacing: 0; }
      .section { font-size: 19px; font-weight: 800; }
      .axis { stroke: #d7dde7; stroke-width: 1; }
      .grid { stroke: #e7ebf1; stroke-width: 1; }
      .releaseBar { stroke-width: 0; }
      .remainder { fill: #eef1f5; stroke: #cfd6e1; stroke-width: 1; }
      .completion { fill-opacity: 1; }
      .dot { fill: #ffffff; stroke-width: 3; }
      .notStarted { fill: #ffffff; stroke-width: 2; stroke-dasharray: 7 5; }
      .soft { fill: #f8fafc; stroke: #e1e6ee; }
    </style>
  </defs>

  <rect class="page" x="0" y="0" width="{{.Width}}" height="{{.Height}}"/>

  <text class="ink title" x="56" y="58">{{.ProjectName}} Program Portfolio Report</text>
  <text class="muted" x="56" y="86" font-size="14">Generated {{.GeneratedAt}} · Six-month window: {{.WindowLabel}}</text>

  <rect class="panel" x="40" y="118" width="1120" height="390" rx="8"/>
  <text class="ink section" x="64" y="154">Release Timeline</text>
  <text class="muted small" x="64" y="176">Each release bar uses Fix Version dates from the CSV. The dot marks the release end date.</text>
  <line class="axis" x1="{{.ChartLeft}}" y1="194" x2="{{.ChartRight}}" y2="194"/>
  {{range .MonthTicks}}
    <line class="grid" x1="{{.X}}" y1="194" x2="{{.X}}" y2="482"/>
    <text class="muted small" x="{{.X}}" y="184" text-anchor="middle">{{.Label}}</text>
  {{end}}
  <line x1="{{.TodayX}}" y1="194" x2="{{.TodayX}}" y2="482" stroke="#ba3a3a" stroke-width="2"/>
  <text class="muted small" x="{{.TodayX}}" y="498" text-anchor="middle">as of</text>

  {{range .Releases}}
    <text class="ink label" x="64" y="{{.Y}}">{{.Name}}</text>
    <text class="muted small" x="64" y="{{.Y}}" dy="20">{{.DateRange}} · {{.Done}}/{{.Total}} done</text>
    <rect class="remainder" x="{{.X}}" y="{{.Y}}" width="{{.Width}}" height="18" rx="9"/>
    <rect class="releaseBar" x="{{.X}}" y="{{.Y}}" width="{{.FillWidth}}" height="18" rx="9" fill="{{.Color}}"/>
    <circle class="dot" cx="{{.DotX}}" cy="{{.Y}}" r="7" dy="9" transform="translate(0 9)" stroke="{{.Color}}"/>
    <text class="ink label" x="{{.DotX}}" dx="16" y="{{.Y}}" dy="14">{{.Percent}}%</text>
  {{end}}

  <rect class="panel" x="40" y="532" width="1120" height="250" rx="8"/>
  <text class="ink section" x="64" y="568">Important Epics</text>
  <text class="muted small" x="64" y="590">{{.RankingLabel}}. Only epics that intersect the six-month window are eligible.</text>
  {{range .TopEpics}}
    <rect class="soft" x="60" y="{{.Y}}" width="1080" height="54" rx="7"/>
    <text class="ink label" x="82" y="{{.Y}}" dy="22">#{{.Rank}} {{.Key}} · {{.Name}}</text>
    <text class="muted small" x="82" y="{{.Y}}" dy="40">{{.MetricLabel}} · {{.DateRange}}</text>
    <rect class="remainder" x="{{.X}}" y="{{.Y}}" width="{{.Width}}" height="16" rx="8"/>
    {{if .NotStarted}}
      <rect class="notStarted" x="{{.X}}" y="{{.Y}}" width="{{.Width}}" height="16" rx="8" stroke="{{.Color}}"/>
    {{else}}
      <rect class="completion" x="{{.X}}" y="{{.Y}}" width="{{.FillWidth}}" height="16" rx="8" fill="{{.Color}}"/>
    {{end}}
    <circle class="dot" cx="{{.X}}" cy="{{.Y}}" r="5" transform="translate(0 8)" stroke="{{.Color}}"/>
    <circle class="dot" cx="{{.X}}" cy="{{.Y}}" r="5" transform="translate({{.Width}} 8)" stroke="{{.Color}}"/>
    <text class="ink label" x="{{.X}}" dx="{{.Width}}" y="{{.Y}}" dy="13">{{.Percent}}%</text>
  {{end}}
</svg>
`

const sampleCSV = `Key,Summary,Issue Type,Status,Assignee,Fix Version/s,Epic Link,Parent,Start Date,Due Date,Story Points,Planned,Depends On,Blocks
PAY-101,Payments foundation,Epic,In Progress,Mina,2026.6,,PAY-101,2026-05-20,2026-07-14,13,true,,
PAY-111,Ledger schema,Story,Done,Ari,2026.6,PAY-101,,2026-05-20,2026-06-05,5,true,,
PAY-112,Payment retry worker,Story,In Progress,Sam,2026.6,PAY-101,,2026-06-06,2026-06-28,8,true,PAY-111,PAY-122
PAY-113,Settlement audit view,Story,To Do,Lee,2026.7,PAY-101,,2026-06-28,2026-07-14,5,true,PAY-112,
RPT-201,Reporting modernization,Epic,To Do,Noor,2026.7,,RPT-201,2026-06-20,2026-08-22,21,true,PAY-101,
RPT-211,Metric catalog,Story,To Do,Noor,2026.7,RPT-201,,2026-06-20,2026-07-08,8,true,PAY-113,
RPT-212,Dashboard export,Story,To Do,Ivy,2026.8,RPT-201,,2026-07-09,2026-08-02,8,true,RPT-211,
RPT-213,Report subscriptions,Story,To Do,Ivy,2026.8,RPT-201,,2026-08-03,2026-08-22,5,true,RPT-212,
SEC-301,Security hardening,Epic,In Progress,Chen,2026.6,,SEC-301,2026-05-18,2026-07-03,18,true,,
SEC-311,Token rotation,Story,Done,Chen,2026.6,SEC-301,,2026-05-18,2026-05-31,5,true,,
SEC-312,Permission review,Story,In Progress,Bo,2026.6,SEC-301,,2026-06-01,2026-06-18,8,true,SEC-311,
SEC-313,Admin alerting,Story,To Do,Bo,2026.7,SEC-301,,2026-06-19,2026-07-03,5,true,SEC-312,RPT-211
MOB-401,Mobile launch readiness,Epic,To Do,June,2026.9,,MOB-401,2026-08-15,2026-10-20,20,true,RPT-201,
MOB-411,Offline receipts,Story,To Do,June,2026.9,MOB-401,,2026-08-15,2026-09-05,8,true,RPT-213,
MOB-412,Push notifications,Story,To Do,June,2026.9,MOB-401,,2026-09-06,2026-09-24,5,true,MOB-411,
MOB-413,Release candidate,Story,To Do,June,2026.10,MOB-401,,2026-09-25,2026-10-20,7,true,MOB-412,
OPS-501,Operational readiness,Epic,To Do,Tess,2026.10,,OPS-501,2026-09-10,2026-11-10,13,true,SEC-301,
OPS-511,Runbooks,Story,To Do,Tess,2026.10,OPS-501,,2026-09-10,2026-09-30,5,true,SEC-313,
OPS-512,Launch dashboard,Story,To Do,Tess,2026.10,OPS-501,,2026-10-01,2026-10-25,5,true,OPS-511,
OPS-513,Go-live checklist,Story,To Do,Tess,2026.11,OPS-501,,2026-10-26,2026-11-10,3,true,OPS-512,
`

const reportTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>JiraViz Report</title>
<style>
:root {
  --ink: #202124;
  --muted: #667085;
  --line: #d9dde5;
  --paper: #f7f8fb;
  --panel: #ffffff;
  --green: #247c5b;
  --green-soft: #cbe9db;
  --amber: #b86b00;
  --amber-soft: #ffe2af;
  --red: #ba3a3a;
  --blue: #2d6cdf;
  --blue-soft: #d7e5ff;
  --teal: #187a83;
  --violet: #7556b7;
  --shadow: 0 8px 28px rgba(32, 33, 36, .08);
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: var(--ink);
  background: var(--paper);
}
header {
  padding: 28px 32px 18px;
  background: #fff;
  border-bottom: 1px solid var(--line);
}
h1, h2, h3, p { margin: 0; }
h1 { font-size: 30px; letter-spacing: 0; }
h2 { font-size: 20px; margin-bottom: 12px; }
h3 { font-size: 15px; margin-bottom: 8px; }
.subhead { color: var(--muted); margin-top: 6px; }
.summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  padding: 18px 32px;
}
.metric {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 14px;
  box-shadow: var(--shadow);
}
.metric strong { display: block; font-size: 24px; }
.metric span { color: var(--muted); font-size: 13px; }
main { padding: 0 32px 36px; }
.option {
  margin-top: 22px;
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 8px;
  box-shadow: var(--shadow);
  overflow: hidden;
}
.option > .title {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: baseline;
  padding: 18px 20px;
  border-bottom: 1px solid var(--line);
}
.title small { color: var(--muted); }
.content { padding: 18px 20px 22px; }
.release-list { display: grid; gap: 12px; }
.release-row {
  display: grid;
  grid-template-columns: minmax(120px, 180px) 1fr minmax(90px, auto);
  gap: 12px;
  align-items: center;
}
.version-name { font-weight: 700; }
.progress {
  height: 18px;
  border: 1px solid #c7ced9;
  background: #eef1f5;
  border-radius: 999px;
  overflow: hidden;
}
.fill {
  height: 100%;
  background: linear-gradient(90deg, var(--green), var(--teal));
}
.release-meta { color: var(--muted); font-size: 13px; }
.split {
  display: grid;
  grid-template-columns: minmax(0, 1.05fr) minmax(300px, .95fr);
  gap: 18px;
}
.path {
  display: grid;
  gap: 10px;
}
.path-node {
  border-left: 5px solid var(--blue);
  background: #f6f9ff;
  padding: 12px;
  border-radius: 8px;
}
.path-node strong { display: block; }
.path-node span { color: var(--muted); font-size: 13px; }
.arrow { color: var(--muted); padding-left: 16px; }
.train {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 14px;
}
.car {
  min-height: 144px;
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 14px;
  background: #fbfcfe;
}
.ring {
  --p: 0;
  width: 76px;
  height: 76px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  margin: 10px 0;
  background: conic-gradient(var(--green) calc(var(--p) * 1%), #e7ebf1 0);
}
.ring span {
  width: 54px;
  height: 54px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: #fff;
  font-weight: 800;
  font-size: 14px;
}
.heatmap {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 10px;
}
.tile {
  border: 1px solid var(--line);
  border-radius: 8px;
  min-height: 110px;
  padding: 12px;
  background: #f8fafb;
}
.tile.done-high { background: #e6f4ec; }
.tile.done-mid { background: #fff6df; }
.tile.done-low { background: #ffe8e5; }
.tile strong { display: block; font-size: 20px; margin-top: 6px; }
.timeline {
  display: grid;
  gap: 9px;
}
.epic-line {
  display: grid;
  grid-template-columns: minmax(170px, 240px) 1fr minmax(70px, auto);
  gap: 12px;
  align-items: center;
}
.lane {
  height: 24px;
  background: repeating-linear-gradient(90deg, #f0f3f7 0, #f0f3f7 38px, #e3e8ef 39px, #e3e8ef 40px);
  border-radius: 5px;
  overflow: hidden;
  border: 1px solid #d7dde6;
}
.duration {
  height: 100%;
  min-width: 12px;
  background: linear-gradient(90deg, var(--blue), var(--violet));
}
.print-table {
  width: 100%;
  border-collapse: collapse;
}
.print-table th, .print-table td {
  border-bottom: 1px solid var(--line);
  padding: 10px 8px;
  text-align: left;
  vertical-align: top;
}
.print-table th { color: var(--muted); font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
.badge {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--blue-soft);
  color: #17488f;
  font-size: 12px;
  font-weight: 700;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
  color: var(--muted);
  font-size: 12px;
}
.legend i {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 2px;
  margin-right: 4px;
}
@media (max-width: 760px) {
  header, main { padding-left: 16px; padding-right: 16px; }
  .summary, .split { grid-template-columns: 1fr; padding-left: 16px; padding-right: 16px; }
  .release-row, .epic-line { grid-template-columns: 1fr; }
}
</style>
</head>
<body>
<header>
  <h1>{{.GeneratedAt}} JiraViz Project Report</h1>
  <p class="subhead">FixVersion completion from planned issues, plus epic critical path for {{.NextSixMonthsLabel}}.</p>
</header>

<section class="summary">
  <div class="metric"><strong>{{.IssueCount}}</strong><span>Total issues imported</span></div>
  <div class="metric"><strong>{{.PlannedIssueCount}}</strong><span>Planned issues measured</span></div>
  <div class="metric"><strong>{{len .FixVersions}}</strong><span>FixVersions</span></div>
  <div class="metric"><strong>{{.TopCriticalPath.TotalDays}}d</strong><span>Longest epic path</span></div>
</section>

<main>
  <section class="option">
    <div class="title"><h2>Option 1: Executive Split View</h2><small>Release health on the left, critical path on the right</small></div>
    <div class="content split">
      <div class="release-list">
        {{range .FixVersions}}
        <div class="release-row">
          <div><div class="version-name">{{.Name}}</div><div class="release-meta">{{.Done}}/{{.Total}} planned done</div></div>
          <div class="progress"><div class="fill" style="width: {{.Percent}}%"></div></div>
          <strong>{{.Percent}}%</strong>
        </div>
        {{end}}
      </div>
      <div>
        <h3>Critical Path: {{.TopCriticalPath.TotalDays}} days</h3>
        <div class="path">
          {{range $i, $epic := .TopCriticalPath.Epics}}
            {{if $i}}<div class="arrow">downstream dependency</div>{{end}}
            <div class="path-node"><strong>{{$epic.Key}} {{$epic.Summary}}</strong><span>{{date $epic.Start}} - {{date $epic.Due}} · {{$epic.Percent}}% done · {{$epic.IssueCount}} issues</span></div>
          {{end}}
        </div>
      </div>
    </div>
  </section>

  <section class="option">
    <div class="title"><h2>Option 2: Release Train</h2><small>Each fixVersion becomes a release car with completion rings</small></div>
    <div class="content train">
      {{range .FixVersions}}
      <article class="car">
        <h3>{{.Name}}</h3>
        <div class="ring" style="--p: {{.Percent}}"><span>{{.Percent}}%</span></div>
        <p class="release-meta">{{.Open}} open · {{printf "%.0f" .DonePoints}}/{{printf "%.0f" .StoryPoints}} pts done · {{.Blocked}} with dependencies</p>
      </article>
      {{end}}
    </div>
  </section>

  <section class="option">
    <div class="title"><h2>Option 3: Portfolio Heatmap</h2><small>Quick scan for releases that are falling behind</small></div>
    <div class="content heatmap">
      {{range .FixVersions}}
      <article class="tile {{if ge .Percent 75}}done-high{{else if ge .Percent 40}}done-mid{{else}}done-low{{end}}">
        <div class="version-name">{{.Name}}</div>
        <strong>{{.Percent}}%</strong>
        <p class="release-meta">{{.Done}} done, {{.Open}} open, {{.Total}} planned</p>
      </article>
      {{end}}
    </div>
    <div class="content legend"><span><i style="background:#e6f4ec"></i>75%+</span><span><i style="background:#fff6df"></i>40-74%</span><span><i style="background:#ffe8e5"></i>Below 40%</span></div>
  </section>

  <section class="option">
    <div class="title"><h2>Option 4: Six-Month Epic Critical Path</h2><small>Microsoft Project-like lane view, simplified to epic bars</small></div>
    <div class="content timeline">
      {{range .Epics}}
      <div class="epic-line">
        <div><strong>{{.Key}}</strong><div class="release-meta">{{.Summary}}</div></div>
        <div class="lane"><div class="duration" style="width: {{daysWidth .DurationDays $.MaxEpicDuration}}%"></div></div>
        <div class="badge">{{.DurationDays}}d</div>
      </div>
      {{end}}
    </div>
  </section>

  <section class="option">
    <div class="title"><h2>Option 5: Compact Board Report</h2><small>Printable table combining releases and top dependency chains</small></div>
    <div class="content">
      <table class="print-table">
        <thead><tr><th>FixVersion</th><th>Completion</th><th>Status mix</th><th>Planned issues</th></tr></thead>
        <tbody>
        {{range .FixVersions}}
          <tr>
            <td><strong>{{.Name}}</strong></td>
            <td><span class="badge">{{.Percent}}%</span></td>
            <td>{{range .StatusCounts}}<span class="badge">{{.Name}} {{.Count}}</span> {{end}}</td>
            <td>{{.Total}}</td>
          </tr>
        {{end}}
        </tbody>
      </table>
      <h3 style="margin-top:18px">Top Critical Paths</h3>
      <table class="print-table">
        <thead><tr><th>Rank</th><th>Path</th><th>Duration</th><th>Points</th></tr></thead>
        <tbody>
        {{range $i, $path := .CriticalPaths}}
          <tr><td>{{printf "#%d" (add $i 1)}}</td><td>{{joinEpics $path.Epics}}</td><td>{{$path.TotalDays}}d</td><td>{{printf "%.0f" $path.TotalPoints}}</td></tr>
        {{end}}
        </tbody>
      </table>
    </div>
  </section>
</main>
</body>
</html>`
