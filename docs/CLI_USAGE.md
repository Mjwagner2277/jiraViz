# JiraViz CLI Usage Guide

This guide is for people who have never used a command-line tool before. JiraViz reads a Jira CSV export and creates an SVG image report.

## What You Will Make

JiraViz creates a program portfolio report like this:

![Program portfolio report example](images/program-portfolio-report.png)

The report shows:

- Release timelines from Jira Fix Version dates
- Percent complete for each release
- Important Epics over the next six months

## Before You Start

You need:

- A Mac, Linux, or Windows computer with a terminal
- The JiraViz project folder
- Go installed
- A Jira CSV export, or one of the sample CSV files in this repo

If you are using the sample data, you do not need Jira yet.

## Step 1: Open a Terminal

On macOS:

1. Open Finder.
2. Go to the JiraViz project folder.
3. Open Terminal.
4. Type `cd `, including the space after `cd`.
5. Drag the JiraViz folder into the Terminal window.
6. Press `Return`.

You should now be inside the JiraViz folder.

You can check by running:

```bash
pwd
```

The output should end with something like:

```text
JiraViz
```

## Step 2: See the Available Projects

The sample multi-project CSV has several construction projects in one file.

Run:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -list-projects
```

You should see:

```text
Projects:
- MAP (14 issues)
- OAK (14 issues)
- PINE (10 issues)
```

This tells you which project names you can pass to `-project`.

## Step 3: Create a Report

To create a report for the Pine Ridge project, run:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project PINE -out pine-report.svg
```

This creates a file named:

```text
pine-report.svg
```

You can open that SVG file in a browser, Preview, or any tool that can display SVG images.

## Step 4: Create Reports for Other Projects

Use the same command and change only the `-project` value and output file name.

Oak Street:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project OAK -out oak-report.svg
```

Maple Avenue:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project MAP -out maple-report.svg
```

## Command Pattern

Most JiraViz commands follow this pattern:

```bash
go run . -input YOUR_CSV_FILE.csv -project PROJECT_NAME -out OUTPUT_FILE.svg
```

Example:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project OAK -out oak-report.svg
```

## Flags

Flags are the words that start with a dash, such as `-input` or `-project`.

| Flag | Required? | What it does | Example |
| --- | --- | --- | --- |
| `-input` | Yes | Path to the Jira CSV file | `-input mockups/construction-company-multi-project-jira.csv` |
| `-project` | Usually | Chooses one project from the CSV | `-project PINE` |
| `-out` | No | Names the SVG report file | `-out pine-report.svg` |
| `-list-projects` | No | Lists projects in the CSV and exits | `-list-projects` |
| `-mode` | No | Chooses how Important Epics are ranked | `-mode issues` |
| `-as-of` | No | Sets the report date for repeatable output | `-as-of 2026-05-16` |
| `-sample` | No | Writes a sample CSV to the input path before rendering | `-sample` |
| `-version` | No | Prints the JiraViz version | `-version` |

## Ranking Important Epics

By default, JiraViz ranks Important Epics by story points when story points exist.

Default ranking:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project PINE -out pine-report.svg
```

Force ranking by planned issue count:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project PINE -mode issues -out pine-report-by-issues.svg
```

Force ranking by story points:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project PINE -mode points -out pine-report-by-points.svg
```

## Make the Same Image Every Time

Reports normally use today as the start of the six-month window. If you want the same dates every time, use `-as-of`.

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -project PINE -out pine-report.svg -as-of 2026-05-16
```

This is useful for demos, screenshots, documentation, and tests.

## Using Your Own Jira CSV Export

Export issues from Jira as CSV, then run:

```bash
go run . -input path/to/your-jira-export.csv -list-projects
```

Then choose a project:

```bash
go run . -input path/to/your-jira-export.csv -project YOURPROJECT -out your-project-report.svg
```

## Expected CSV Fields

JiraViz accepts common Jira CSV column names. The sample file uses:

```text
Project
Key
Summary
Issue Type
Status
Assignee
Fix Version/s
Epic Link
Parent
Start Date
Due Date
Story Points
Planned
Depends On
Blocks
```

The most important fields are:

- `Project`: lets you choose a project with `-project`
- `Key`: Jira issue key, such as `PINE-100`
- `Issue Type`: Epic, Story, Task, etc.
- `Status`: used to calculate percent complete
- `Fix Version/s`: used for release rows
- `Epic Link` or `Parent`: connects stories/tasks to epics
- `Start Date` and `Due Date`: used for the timeline bars
- `Story Points`: used to rank Important Epics
- `Planned`: marks whether an issue should count in the report

If there is no `Project` column, JiraViz can infer the project from the issue key prefix. For example, `PINE-100` becomes project `PINE`.

## Opening the Output

On macOS, you can open the SVG from Terminal:

```bash
open pine-report.svg
```

Or open it manually:

1. Find `pine-report.svg` in Finder.
2. Double-click it.
3. If prompted, open it with Safari, Chrome, Preview, or another SVG viewer.

## Common Problems

### I See "multiple projects found"

The CSV has more than one project. List the projects first:

```bash
go run . -input mockups/construction-company-multi-project-jira.csv -list-projects
```

Then run again with `-project`.

### I Do Not Know My Project Name

Use:

```bash
go run . -input your-file.csv -list-projects
```

### My Report Has Missing Dates

Check that your CSV has `Start Date` and `Due Date` columns. JiraViz can still render without them, but the timeline will be less meaningful.

### My Important Epics Look Wrong

Check whether your CSV has story points.

If you do not use story points, rank by issue count:

```bash
go run . -input your-file.csv -project YOURPROJECT -mode issues -out report.svg
```

## More Example Visuals

### Kanban Top Epics

![Kanban top epics example](images/kanban-top-epics.png)

### All Epics Six-Month Gantt

![All epics six-month Gantt example](images/all-epics-six-month-gantt.png)
